// Resumo da sessão do Tempo real: a cada atualização guarda, por aplicação, só agregados (mín/máx/média e
// contagens), nunca as séries — o consumo de memória não cresce com o tempo, só com o número de aplicações.
// Lógica pura (testada em metricsLiveSession.test.ts); só o download toca o DOM (downloadText).
import { REPORT_TZ, fmtLatency } from "./metricsReport";
import { fmtBytesPerSec, lastValue, type LiveSeries } from "./metricsLive";

export interface Agg {
  n: number;
  min: number;
  max: number;
  sum: number;
}

export interface AppStat {
  cluster: string;
  namespace: string;
  workload: string;
  samples: number;
  rps: Agg | null;
  err5xx: Agg | null; // taxa por segundo, na hora da amostra
  err4xx: Agg | null;
  avg: Agg | null; // tempo de resposta médio, em ms
  lat: Agg | null; // percentil escolhido, em ms
  replicas: Agg | null;
  cpuLimPct: Agg | null;
  memLimPct: Agg | null;
  throttlePct: Agg | null;
  netRx: Agg | null;
  netTx: Agg | null;
  restartsMax: number; // maior contagem de restarts (janela de 15 min) vista numa amostra
  restartReasons: Record<string, number>; // maior nº de pods por razão visto numa amostra
  pendingMax: number;
  notReadyMax: number;
  atCeiling: number; // amostras com as réplicas desejadas no teto do HPA
  errors: number; // amostras em que a coleta da aplicação falhou
}

export interface Session {
  startedAt: number;
  updates: number; // atualizações registradas
  percentile: 50 | 95 | 99;
  apps: Map<string, AppStat>;
}

export function newSession(startedAt: number, percentile: 50 | 95 | 99 = 99): Session {
  return { startedAt, updates: 0, percentile, apps: new Map() };
}

const add = (a: Agg | null, v: number | null | undefined): Agg | null => {
  if (v == null || !Number.isFinite(v)) return a;
  return a ? { n: a.n + 1, min: Math.min(a.min, v), max: Math.max(a.max, v), sum: a.sum + v } : { n: 1, min: v, max: v, sum: v };
};

const keyOf = (s: { cluster: string; namespace: string; workload: string }) => `${s.cluster}|${s.namespace}|${s.workload}`;

/** Registra uma atualização. Muda a sessão (barato: um passo por aplicação). */
export function recordSample(sess: Session, series: LiveSeries[], percentile: 50 | 95 | 99): void {
  sess.updates += 1;
  sess.percentile = percentile;
  for (const s of series) {
    const k = keyOf(s);
    let a = sess.apps.get(k);
    if (!a) {
      a = {
        cluster: s.cluster, namespace: s.namespace, workload: s.workload, samples: 0, rps: null, err5xx: null, err4xx: null, avg: null, lat: null, replicas: null,
        cpuLimPct: null, memLimPct: null, throttlePct: null, netRx: null, netTx: null, restartsMax: 0, restartReasons: {}, pendingMax: 0, notReadyMax: 0, atCeiling: 0, errors: 0,
      };
      sess.apps.set(k, a);
    }
    a.samples += 1;
    if (s.error && !s.resources) {
      a.errors += 1;
      continue;
    }
    a.rps = add(a.rps, lastValue(s.rps));
    a.err5xx = add(a.err5xx, lastValue(s.err_5xx_rps));
    a.err4xx = add(a.err4xx, lastValue(s.err_4xx_rps));
    a.avg = add(a.avg, lastValue(s.avg_ms));
    a.lat = add(a.lat, lastValue(percentile === 99 ? s.p99_ms : percentile === 95 ? s.p95_ms : s.p50_ms));
    const r = s.resources;
    if (!r) continue;
    a.replicas = add(a.replicas, r.replicas_available);
    a.cpuLimPct = add(a.cpuLimPct, r.cpu_lim_pct);
    a.memLimPct = add(a.memLimPct, r.mem_lim_pct);
    a.throttlePct = add(a.throttlePct, r.cpu_throttle_pct);
    a.netRx = add(a.netRx, r.net_rx_bps);
    a.netTx = add(a.netTx, r.net_tx_bps);
    a.restartsMax = Math.max(a.restartsMax, r.restarts_15m ?? 0);
    for (const [reason, n] of Object.entries(r.restart_reasons ?? {})) a.restartReasons[reason] = Math.max(a.restartReasons[reason] ?? 0, n);
    a.pendingMax = Math.max(a.pendingMax, r.pods_pending ?? 0);
    a.notReadyMax = Math.max(a.notReadyMax, r.pods_not_ready ?? 0);
    if (r.hpa_max != null && r.replicas_desired != null && r.hpa_max > 0 && r.replicas_desired >= r.hpa_max) a.atCeiling += 1;
  }
}

const NO = "sem dado";
const num = (v: number, d = 1) => v.toLocaleString("pt-BR", { maximumFractionDigits: d });
const avg = (a: Agg) => a.sum / a.n;

/** "mín 10 · méd 20 · máx 30" com o formatador dado; sem amostra = sem dado. */
function range(a: Agg | null, f: (v: number) => string): string {
  return a ? `mín ${f(a.min)} · méd ${f(avg(a))} · máx ${f(a.max)}` : NO;
}

const fmtWhen = (ms: number) => new Date(ms).toLocaleString("pt-BR", { timeZone: REPORT_TZ });
const fmtDur = (ms: number) => {
  const m = Math.max(0, Math.round(ms / 60000));
  return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`;
};
const sorted = (sess: Session) =>
  [...sess.apps.values()].sort((a, b) => a.cluster.localeCompare(b.cluster, "pt-BR") || a.namespace.localeCompare(b.namespace, "pt-BR") || a.workload.localeCompare(b.workload, "pt-BR"));
const reasons = (m: Record<string, number>) =>
  Object.entries(m).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([r, n]) => `${r} ×${n}`).join(", ");

/** Resumo em Markdown: uma seção por cluster, uma linha por aplicação. */
export function buildSessionMarkdown(sess: Session, nowMs: number): string {
  const lines: string[] = [
    "# Resumo da sessão do Tempo real",
    "",
    `- **Início:** ${fmtWhen(sess.startedAt)} (${REPORT_TZ})`,
    `- **Fim do resumo:** ${fmtWhen(nowMs)} · duração ${fmtDur(nowMs - sess.startedAt)}`,
    `- **Atualizações registradas:** ${sess.updates}`,
    `- **Percentil de latência:** P${sess.percentile}`,
    "",
    "> Os valores são **amostrados a cada atualização** (o valor mais recente de cada métrica naquele momento), não o pico entre uma atualização e outra. Picos curtos podem não aparecer. Tráfego, erros e latência vêm do Dynatrace; réplicas, CPU, memória, throttling, rede e restarts, do Prometheus.",
    "",
  ];
  let cluster = "";
  for (const a of sorted(sess)) {
    if (a.cluster !== cluster) {
      cluster = a.cluster;
      lines.push(`## ${cluster}`, "");
    }
    lines.push(`### ${a.workload} (ns: ${a.namespace})`, "");
    lines.push(`- **Amostras:** ${a.samples}${a.errors ? ` (${a.errors} com falha de coleta)` : ""}`);
    lines.push(`- **RPS:** ${range(a.rps, (v) => num(v))}`);
    lines.push(`- **Erros 5xx/s:** ${range(a.err5xx, (v) => num(v, 2))}`);
    lines.push(`- **Erros 4xx/s:** ${range(a.err4xx, (v) => num(v, 2))}`);
    lines.push(`- **Resposta média:** ${range(a.avg, (v) => fmtLatency(v))}`);
    lines.push(`- **P${sess.percentile}:** ${range(a.lat, (v) => fmtLatency(v))}`);
    lines.push(`- **Réplicas disponíveis:** ${range(a.replicas, (v) => num(v, 0))}${a.atCeiling ? ` · **no teto do HPA em ${a.atCeiling} de ${a.samples} amostras**` : ""}`);
    lines.push(`- **CPU % do limit:** ${range(a.cpuLimPct, (v) => `${num(v)}%`)}`);
    lines.push(`- **Memória % do limit:** ${range(a.memLimPct, (v) => `${num(v)}%`)}`);
    lines.push(`- **Throttling de CPU:** ${range(a.throttlePct, (v) => `${num(v)}%`)}`);
    lines.push(`- **Rede ↓ / ↑:** ${a.netRx ? `↓ ${range(a.netRx, fmtBytesPerSec)}` : NO}${a.netTx ? ` · ↑ ${range(a.netTx, fmtBytesPerSec)}` : ""}`);
    lines.push(`- **Restarts (maior em 15 min):** ${a.restartsMax}${reasons(a.restartReasons) ? ` (${reasons(a.restartReasons)})` : ""}`);
    if (a.pendingMax || a.notReadyMax) lines.push(`- **Pods:** até ${a.pendingMax} pendente(s) e ${a.notReadyMax} não pronto(s) numa amostra`);
    lines.push("");
  }
  if (sess.apps.size === 0) lines.push("_Nenhuma aplicação foi acompanhada nesta sessão._", "");
  return lines.join("\n");
}

const csvText = (s: string) => `"${(/^[=+\-@\t\r]/.test(s) ? `'${s}` : s).replace(/"/g, '""')}"`;
const csvNum = (v: number | null | undefined, d = 2) => (v == null ? "" : String(Math.round(v * 10 ** d) / 10 ** d));

/** CSV com um registro por aplicação; números crus (ponto decimal), sem dado = vazio; BOM para o Excel. */
export function buildSessionCSV(sess: Session, delimiter = ";"): string {
  const cols: [string, (a: AppStat) => string][] = [];
  const aggCols = (name: string, pick: (a: AppStat) => Agg | null, d: number) => {
    cols.push([`${name}_min`, (a) => csvNum(pick(a)?.min, d)], [`${name}_med`, (a) => { const g = pick(a); return g ? csvNum(avg(g), d) : ""; }], [`${name}_max`, (a) => csvNum(pick(a)?.max, d)]);
  };
  cols.push(["cluster", (a) => csvText(a.cluster)], ["namespace", (a) => csvText(a.namespace)], ["deployment", (a) => csvText(a.workload)], ["amostras", (a) => String(a.samples)], ["amostras_com_falha", (a) => String(a.errors)]);
  aggCols("rps", (a) => a.rps, 2);
  aggCols("erros_5xx_ps", (a) => a.err5xx, 3);
  aggCols("erros_4xx_ps", (a) => a.err4xx, 3);
  aggCols("resposta_media_ms", (a) => a.avg, 1);
  aggCols(`p${sess.percentile}_ms`, (a) => a.lat, 1);
  aggCols("replicas", (a) => a.replicas, 0);
  aggCols("cpu_limit_pct", (a) => a.cpuLimPct, 1);
  aggCols("mem_limit_pct", (a) => a.memLimPct, 1);
  aggCols("throttling_pct", (a) => a.throttlePct, 2);
  aggCols("rede_rx_bps", (a) => a.netRx, 0);
  aggCols("rede_tx_bps", (a) => a.netTx, 0);
  cols.push(["restarts_max_15m", (a) => String(a.restartsMax)], ["razoes_restart", (a) => csvText(reasons(a.restartReasons))], ["pods_pendentes_max", (a) => String(a.pendingMax)], ["pods_nao_prontos_max", (a) => String(a.notReadyMax)], ["amostras_no_teto_hpa", (a) => String(a.atCeiling)]);
  const lines = [cols.map(([h]) => csvText(h)).join(delimiter)];
  for (const a of sorted(sess)) lines.push(cols.map(([, f]) => f(a)).join(delimiter));
  return "\uFEFF" + lines.join("\r\n") + "\r\n";
}
