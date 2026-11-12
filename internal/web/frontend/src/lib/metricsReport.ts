// Relatório de métricas por aplicação (aba "Relatório de Métricas"): tipos da API, janela de tempo em
// fuso, agrupamento da lista de HPAs monitorados e formatação. Lógica pura (testada em
// metricsReport.test.ts); nenhuma chamada de rede aqui.

// ── tipos da API (espelham internal/monitoring/report) ─────────────────────────────────────────────

export interface ReportItem {
  cluster: string;
  namespace: string;
  workload: string;
}

export interface ReportPct {
  max: number;
  min: number;
  avg: number;
}

export interface ReportTraffic {
  source: string;
  requests: number | null;
  max_rps: number | null;
  p50_peak_ms: number | null;
  p95_peak_ms: number | null;
  p99_peak_ms: number | null;
  p99_service?: string;
  err_4xx: number | null;
  err_5xx: number | null;
  services: string[] | null;
  peak_approx: boolean;
  resolution: string;
  notes?: string[];
}

export interface ReportUsage {
  source: string;
  cpu_req_pct: ReportPct | null;
  cpu_lim_pct: ReportPct | null;
  mem_req_pct: ReportPct | null;
  mem_lim_pct: ReportPct | null;
  step: string;
  notes?: string[];
}

export type ReportFlag = "" | "warn" | "red";

export interface ReportPool {
  name: string;
  nodes: number;
  instance_type?: string;
}

export interface ReportNodeUsage {
  nodes_with_data: number;
  worst_pct: number | null;
  avg_pct: number | null;
}

/** Contexto de node: em que pool a aplicação rodou, quão cheios estavam os nodes e o throttling de CPU. */
export interface ReportNodeContext {
  source: string;
  nodes: number;
  pools: ReportPool[] | null;
  pool_label?: string;
  cpu: ReportNodeUsage | null;
  mem: ReportNodeUsage | null;
  throttle_pct: ReportPct | null;
  step: string;
  notes?: string[];
}

export interface ReportRow extends ReportItem {
  traffic: ReportTraffic | null;
  usage: ReportUsage | null;
  node?: ReportNodeContext | null;
  flags: { p99?: ReportFlag; err_4xx?: ReportFlag; err_5xx?: ReportFlag };
  errors?: string[];
}

export interface ReportPlan {
  dt_resolution: string;
  p99_resolution: string;
  peak_approx: boolean;
  notes?: string[];
}

export interface ReportResult {
  window: { start: string; end: string };
  /** Percentis de latência coletados (50, 95 e/ou 99). */
  percentiles: number[];
  /** O contexto de node foi coletado (colunas de node na tabela e nas exportações). */
  node_context?: boolean;
  plan: ReportPlan;
  warnings?: string[];
  rows: ReportRow[];
  generated_at: string;
}

export type ReportState = "running" | "done" | "failed" | "cancelled";

export interface ReportJob {
  id: string;
  state: ReportState;
  done: number;
  total: number;
  error?: string;
  result?: ReportResult;
  created_at: string;
}

export interface ReportPlanResponse {
  window: { start: string; end: string };
  warnings?: string[] | null;
  plan: ReportPlan;
  duration_seconds: number;
}

// ── alvos: cada alvo é um cluster com seus namespaces e deployments (independente de outras abas) ─────

export function itemKey(cluster: string, namespace: string, workload: string): string {
  return `${cluster}|${namespace}|${workload}`;
}

const cmp = (a: string, b: string) => a.localeCompare(b, "pt-BR");

/** Chave de um deployment dentro de um alvo ("namespace/deployment"). */
export function pickKey(namespace: string, workload: string): string {
  return `${namespace}/${workload}`;
}

/** Alvo do relatório: um cluster, os namespaces escolhidos e os deployments marcados ("namespace/deployment"). */
export interface Target {
  id: string;
  cluster: string;
  namespaces: string[];
  picked: string[];
}

let targetSeq = 0;
export function newTarget(cluster = ""): Target {
  targetSeq += 1;
  return { id: `t${Date.now().toString(36)}${targetSeq}${Math.random().toString(36).slice(2, 6)}`, cluster, namespaces: [], picked: [] };
}

function splitPick(k: string): { namespace: string; workload: string } | null {
  const i = k.indexOf("/");
  return i > 0 && i < k.length - 1 ? { namespace: k.slice(0, i), workload: k.slice(i + 1) } : null;
}

/** Itens do pedido de um alvo (só deployments de namespaces ainda escolhidos). */
export function targetItems(t: Target): ReportItem[] {
  if (!t.cluster) return [];
  const out: ReportItem[] = [];
  for (const k of t.picked) {
    const p = splitPick(k);
    if (p && t.namespaces.includes(p.namespace)) out.push({ cluster: t.cluster, namespace: p.namespace, workload: p.workload });
  }
  return out;
}

/** Todos os itens dos alvos, sem repetição (dois alvos do mesmo cluster podem repetir), ordenados. */
export function allItems(targets: Target[]): ReportItem[] {
  const seen = new Map<string, ReportItem>();
  for (const t of targets) for (const i of targetItems(t)) seen.set(itemKey(i.cluster, i.namespace, i.workload), i);
  return [...seen.values()].sort((a, b) => cmp(a.cluster, b.cluster) || cmp(a.namespace, b.namespace) || cmp(a.workload, b.workload));
}

/** Troca o cluster do alvo; namespaces e deployments eram do cluster anterior e são descartados. */
export function setTargetCluster(t: Target, cluster: string): Target {
  return cluster === t.cluster ? t : { ...t, cluster, namespaces: [], picked: [] };
}

/** Marca ou desmarca um namespace; ao desmarcar, os deployments dele saem da seleção. */
export function toggleTargetNamespace(t: Target, namespace: string, on: boolean): Target {
  if (on) return t.namespaces.includes(namespace) ? t : { ...t, namespaces: [...t.namespaces, namespace].sort(cmp) };
  return { ...t, namespaces: t.namespaces.filter((n) => n !== namespace), picked: t.picked.filter((k) => splitPick(k)?.namespace !== namespace) };
}

/** Marca ou desmarca deployments ("namespace/deployment"); só entram os de namespaces já escolhidos. */
export function togglePicked(t: Target, keys: string[], on: boolean): Target {
  if (!on) {
    const drop = new Set(keys);
    return { ...t, picked: t.picked.filter((k) => !drop.has(k)) };
  }
  const picked = new Set(t.picked);
  for (const k of keys) {
    const p = splitPick(k);
    if (p && t.namespaces.includes(p.namespace)) picked.add(k);
  }
  return { ...t, picked: [...picked].sort(cmp) };
}

/** Aplica a escolha inteira de namespaces; os deployments de namespaces que saíram são descartados. */
export function setTargetNamespaces(t: Target, namespaces: string[]): Target {
  const ns = [...new Set(namespaces)].sort(cmp);
  return { ...t, namespaces: ns, picked: t.picked.filter((k) => ns.includes(splitPick(k)?.namespace ?? "")) };
}

/** Aplica a escolha inteira de deployments ("namespace/deployment"); só valem os de namespaces escolhidos. */
export function setTargetPicked(t: Target, keys: string[]): Target {
  const ok = keys.filter((k) => {
    const p = splitPick(k);
    return !!p && t.namespaces.includes(p.namespace);
  });
  return { ...t, picked: [...new Set(ok)].sort(cmp) };
}

/** Resumo de uma linha do alvo, para o cartão. */
export function targetSummary(t: Target): string {
  if (!t.cluster) return "Escolha um cluster";
  const n = targetItems(t).length;
  if (t.namespaces.length === 0) return "Escolha os namespaces";
  const ns = `${t.namespaces.length} namespace${t.namespaces.length === 1 ? "" : "s"}`;
  return n === 0 ? `${ns}, nenhum deployment marcado` : `${n} deployment${n === 1 ? "" : "s"} em ${ns}`;
}

/** Lê os alvos gravados; ignora o que não tem o formato certo e garante ids únicos. */
export function parseTargets(raw: string | null): Target[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    const ids = new Set<string>();
    const out: Target[] = [];
    for (const x of v) {
      const strs = (a: unknown): a is string[] => Array.isArray(a) && a.every((e) => typeof e === "string");
      if (!x || typeof x.cluster !== "string" || !strs(x.namespaces) || !strs(x.picked)) continue;
      const base = newTarget(x.cluster);
      const id = typeof x.id === "string" && x.id && !ids.has(x.id) ? x.id : base.id;
      ids.add(id);
      out.push({ id, cluster: x.cluster, namespaces: [...x.namespaces], picked: [...x.picked] });
    }
    return out;
  } catch {
    return [];
  }
}

// ── conjuntos salvos (alvos nomeados, no navegador) ────────────────────────────────────────────────

export interface SavedSet {
  name: string;
  targets: Target[];
}
export const SAVED_SETS_KEY = "metrics_report_sets_v2";
export const TARGETS_KEY = "metrics_report_targets";

export function parseSets(raw: string | null): SavedSet[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return v
      .filter((s): s is { name: string; targets: unknown } => !!s && typeof s.name === "string" && Array.isArray(s.targets))
      .map((s) => ({ name: s.name, targets: parseTargets(JSON.stringify(s.targets)).filter((t) => targetItems(t).length > 0) }))
      .filter((s) => s.targets.length > 0);
  } catch {
    return [];
  }
}

/** Grava (ou substitui, pelo nome) um conjunto. Nome vazio ou nenhum item não grava. */
export function upsertSet(sets: SavedSet[], name: string, targets: Target[]): SavedSet[] {
  const n = name.trim();
  const useful = targets.filter((t) => targetItems(t).length > 0);
  if (!n || useful.length === 0) return sets;
  return [...sets.filter((s) => s.name !== n), { name: n, targets: useful.map((t) => ({ ...t, namespaces: [...t.namespaces], picked: [...t.picked] })) }].sort((a, b) => cmp(a.name, b.name));
}

// ── janela de tempo em fuso ────────────────────────────────────────────────────────────────────────

export const REPORT_TZ = "America/Sao_Paulo";

/** Diferença (minutos) entre a hora local do fuso e UTC no instante ts (ms). */
function tzOffsetMinutes(ts: number, tz: string): number {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: tz, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit",
  }).formatToParts(new Date(ts));
  const g = (t: string) => Number(parts.find((p) => p.type === t)?.value);
  const asUTC = Date.UTC(g("year"), g("month") - 1, g("day"), g("hour"), g("minute"), g("second"));
  return Math.round((asUTC - Math.floor(ts / 1000) * 1000) / 60000);
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function offsetLabel(min: number): string {
  const sign = min < 0 ? "-" : "+";
  const a = Math.abs(min);
  return `${sign}${pad(Math.floor(a / 60))}:${pad(a % 60)}`;
}

/**
 * Converte "AAAA-MM-DDTHH:mm" (hora local no fuso) para RFC 3339 com offset, ex.:
 * "2026-10-01T23:00" em America/Sao_Paulo -> "2026-10-01T23:00:00-03:00". null se a data for inválida
 * (formato errado ou dia inexistente, como 30/02).
 */
export function zonedLocalToISO(local: string, tz: string = REPORT_TZ): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(local);
  if (!m) return null;
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  const wall = Date.UTC(y, mo - 1, d, h, mi);
  const chk = new Date(wall);
  if (chk.getUTCFullYear() !== y || chk.getUTCMonth() !== mo - 1 || chk.getUTCDate() !== d || chk.getUTCHours() !== h) return null;
  // duas passagens: o offset de um instante perto de uma troca de horário depende do próprio instante
  const off1 = tzOffsetMinutes(wall - tzOffsetMinutes(wall, tz) * 60000, tz);
  const utc = wall - off1 * 60000;
  const off = tzOffsetMinutes(utc, tz);
  return `${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:00${offsetLabel(off)}`;
}

/** Instante (Date/ISO/ms) -> "AAAA-MM-DDTHH:mm" na hora local do fuso (para preencher os campos). */
export function isoToZonedLocal(ts: number | string | Date, tz: string = REPORT_TZ): string {
  const t = ts instanceof Date ? ts.getTime() : typeof ts === "number" ? ts : Date.parse(ts);
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: tz, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
  }).formatToParts(new Date(t));
  const g = (k: string) => parts.find((p) => p.type === k)?.value ?? "00";
  return `${g("year")}-${g("month")}-${g("day")}T${g("hour")}:${g("minute")}`;
}

export type WindowPreset = "1h" | "3h" | "6h" | "12h" | "24h" | "7d" | "14d";
const PRESET_MS: Record<WindowPreset, number> = {
  "1h": 3600e3, "3h": 3 * 3600e3, "6h": 6 * 3600e3, "12h": 12 * 3600e3, "24h": 24 * 3600e3, "7d": 7 * 24 * 3600e3, "14d": 14 * 24 * 3600e3,
};

/** Janela dos atalhos: termina agora (minuto cheio) e volta N horas. Devolve instantes em ISO UTC. */
export function presetWindow(p: WindowPreset, now: number = Date.now()): { start: string; end: string } {
  const end = Math.floor(now / 60000) * 60000;
  return { start: new Date(end - PRESET_MS[p]).toISOString(), end: new Date(end).toISOString() };
}

// ── percentis de latência escolhidos pelo operador ──────────────────────────────────────────────────

export const PERCENTILE_CHOICES = [50, 95, 99] as const;
export const PERCENTILES_KEY = "metrics_report_percentiles";
export const DEFAULT_PERCENTILES: number[] = [99];

/** Lê a escolha gravada: só 50/95/99, sem repetir, em ordem; vazio ou inválido volta ao padrão (P99). */
export function parsePercentiles(raw: string | null): number[] {
  if (!raw) return [...DEFAULT_PERCENTILES];
  try {
    const v = JSON.parse(raw);
    if (!Array.isArray(v)) return [...DEFAULT_PERCENTILES];
    const ok = PERCENTILE_CHOICES.filter((p) => v.includes(p));
    return ok.length > 0 ? [...ok] : [...DEFAULT_PERCENTILES];
  } catch {
    return [...DEFAULT_PERCENTILES];
  }
}

/** Marca ou desmarca um percentil; sempre sobra pelo menos um (desmarcar o último não faz nada). */
export function togglePercentile(current: number[], p: number, on: boolean): number[] {
  const set = new Set(current);
  if (on) set.add(p);
  else if (set.size > 1) set.delete(p);
  return PERCENTILE_CHOICES.filter((x) => set.has(x));
}

/** Valor do percentil (pico, em ms) de uma linha de tráfego; null quando não foi coletado ou sem dado. */
export function percentileValue(t: ReportTraffic | null | undefined, p: number): number | null {
  if (!t) return null;
  const v = p === 50 ? t.p50_peak_ms : p === 95 ? t.p95_peak_ms : p === 99 ? t.p99_peak_ms : null;
  return v ?? null;
}

// ── contexto de node (opt-in) ───────────────────────────────────────────────────────────────────────

export const NODE_CONTEXT_KEY = "metrics_report_node_context";

/** Lê a escolha gravada do contexto de node: só "true" liga (padrão desligado: custa consultas a mais). */
export function parseNodeContext(raw: string | null): boolean {
  return raw === "true";
}

/** "calculofret2 (27 nodes, Standard_D4s_v5)"; vários pools separados por "; ". */
export function fmtPools(pools: ReportPool[] | null | undefined): string {
  if (!pools || pools.length === 0) return NO_DATA;
  return pools
    .map((p) => `${p.name} (${p.nodes} node${p.nodes === 1 ? "" : "s"}${p.instance_type ? `, ${p.instance_type}` : ""})`)
    .join("; ");
}

/** "76.4% / 22.1%" (pior node / média dos nodes); cobertura parcial aparece como "(N nodes)". */
export function fmtNodeUsage(u: ReportNodeUsage | null | undefined, totalNodes?: number): string {
  if (!u || u.worst_pct == null) return NO_DATA;
  const partial = totalNodes != null && u.nodes_with_data < totalNodes ? ` (${u.nodes_with_data} de ${totalNodes} nodes)` : "";
  return `${fmtPct(u.worst_pct)} / ${fmtPct(u.avg_pct)}${partial}`;
}

/** Throttling é pequeno (frações de %): duas casas, "0.51% / 0.04%" (máx / média). */
export function fmtThrottle(p: ReportPct | null | undefined): string {
  if (!p) return NO_DATA;
  const f = (v: number) => `${(Math.round(v * 100) / 100).toFixed(2)}%`;
  return `${f(p.max)} / ${f(p.avg)}`;
}

// ── resultado agrupado: cluster → namespace → deployment ─────────────────────────────────────────────

export interface ReportNamespaceGroup {
  namespace: string;
  rows: ReportRow[];
  alerts: ReportAlerts;
}
export interface ReportClusterGroup {
  cluster: string;
  namespaces: ReportNamespaceGroup[];
  alerts: ReportAlerts;
}
/** Contagem de linhas por situação: vermelho (algum selo vermelho), aviso (só selo de aviso) e com erro de coleta. */
export interface ReportAlerts {
  total: number;
  red: number;
  warn: number;
  failed: number;
}

function countAlerts(rows: ReportRow[]): ReportAlerts {
  const a: ReportAlerts = { total: rows.length, red: 0, warn: 0, failed: 0 };
  for (const r of rows) {
    const flags = Object.values(r.flags ?? {});
    if (flags.includes("red")) a.red++;
    else if (flags.includes("warn")) a.warn++;
    if ((r.errors?.length ?? 0) > 0) a.failed++;
  }
  return a;
}

/** Agrupa as linhas do relatório por cluster e namespace (ordenado), com a contagem de alertas de cada nível. */
export function groupRows(rows: ReportRow[]): ReportClusterGroup[] {
  const byCluster = new Map<string, Map<string, ReportRow[]>>();
  for (const r of rows) {
    const ns = byCluster.get(r.cluster) ?? new Map<string, ReportRow[]>();
    ns.set(r.namespace, [...(ns.get(r.namespace) ?? []), r]);
    byCluster.set(r.cluster, ns);
  }
  return [...byCluster.entries()]
    .sort(([a], [b]) => cmp(a, b))
    .map(([cluster, ns]) => {
      const namespaces = [...ns.entries()]
        .sort(([a], [b]) => cmp(a, b))
        .map(([namespace, list]) => {
          const sorted = [...list].sort((a, b) => cmp(a.workload, b.workload));
          return { namespace, rows: sorted, alerts: countAlerts(sorted) };
        });
      return { cluster, namespaces, alerts: countAlerts(namespaces.flatMap((n) => n.rows)) };
    });
}

// ── formatação (valor ausente = "sem dado", nunca zero) ─────────────────────────────────────────────

export const NO_DATA = "sem dado";

export function fmtInt(v: number | null | undefined): string {
  return v == null ? NO_DATA : Math.round(v).toLocaleString("pt-BR");
}

export function fmtRps(v: number | null | undefined): string {
  return v == null ? NO_DATA : (Math.round(v * 10) / 10).toLocaleString("pt-BR", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}

/** Latência em segundos com 3 casas ("1.769s"), como no relatório de referência. */
export function fmtLatency(ms: number | null | undefined): string {
  return ms == null ? NO_DATA : `${(ms / 1000).toFixed(3)}s`;
}

export function fmtPct(v: number | null | undefined): string {
  return v == null ? NO_DATA : `${(Math.round(v * 10) / 10).toFixed(1)}%`;
}

/** "9.0% (Min: 1.9%, Avg: 6.7%)" — como na coluna CPU/Mem do relatório. */
export function fmtPctStats(p: ReportPct | null | undefined): string {
  return p == null ? NO_DATA : `${fmtPct(p.max)} (Min: ${fmtPct(p.min)}, Avg: ${fmtPct(p.avg)})`;
}

export const FLAG_ICON: Record<string, string> = { red: "🔴", warn: "⚠️" };
