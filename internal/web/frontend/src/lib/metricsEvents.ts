// Eventos nomeados do Relatório de Métricas: janelas de tempo salvas ("Stress test 11/11 14h-16h"), por
// usuário, no servidor (o navegador guarda só uma cópia). Lógica pura, testada em metricsEvents.test.ts.
import { REPORT_TZ } from "./metricsReport";

export interface SavedEvent {
  name: string;
  /** RFC 3339 (UTC). */
  start: string;
  end: string;
}

export const EVENTS_KEY = "metrics_report_events";
export const MAX_EVENTS = 100;

const valid = (e: unknown): e is SavedEvent => {
  if (!e || typeof e !== "object") return false;
  const { name, start, end } = e as Record<string, unknown>;
  if (typeof name !== "string" || !name.trim() || typeof start !== "string" || typeof end !== "string") return false;
  const s = Date.parse(start), f = Date.parse(end);
  return Number.isFinite(s) && Number.isFinite(f) && f > s;
};

/** Aceita o que veio do servidor ou do navegador (qualquer coisa) e devolve só eventos válidos. */
export function parseEvents(raw: unknown): SavedEvent[] {
  let v = raw;
  if (typeof raw === "string") {
    try {
      v = JSON.parse(raw);
    } catch {
      return [];
    }
  }
  return Array.isArray(v) ? sortEvents(v.filter(valid).map((e) => ({ name: e.name.trim(), start: e.start, end: e.end })).slice(0, MAX_EVENTS)) : [];
}

/** Mais recente primeiro. */
function sortEvents(list: SavedEvent[]): SavedEvent[] {
  return [...list].sort((a, b) => Date.parse(b.start) - Date.parse(a.start) || a.name.localeCompare(b.name, "pt-BR"));
}

/** Grava (ou substitui, pelo nome) um evento. Nome vazio ou janela inválida não grava. */
export function upsertEvent(events: SavedEvent[], name: string, start: string, end: string): SavedEvent[] {
  const n = name.trim();
  const ev = { name: n, start, end };
  if (!n || !valid(ev)) return events;
  return sortEvents([...events.filter((e) => e.name !== n), ev]).slice(0, MAX_EVENTS);
}

export const removeEvent = (events: SavedEvent[], name: string): SavedEvent[] => events.filter((e) => e.name !== name);

/** Junta servidor e navegador sem perder evento: o do servidor vale quando o nome existe nos dois. */
export function mergeEvents(server: SavedEvent[], local: SavedEvent[]): SavedEvent[] {
  const names = new Set(server.map((e) => e.name));
  return sortEvents([...server, ...local.filter((e) => !names.has(e.name))]).slice(0, MAX_EVENTS);
}

// data e hora montadas separadas: o separador que o ICU põe entre elas ("11/11, 14:00") varia entre ambientes
const timeOf = (iso: string) => new Date(iso).toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ, hour: "2-digit", minute: "2-digit" });
const dateOf = (iso: string) => new Date(iso).toLocaleDateString("pt-BR", { timeZone: REPORT_TZ, day: "2-digit", month: "2-digit" });
const fmt = (iso: string, withDate: boolean) => (withDate ? `${dateOf(iso)} ${timeOf(iso)}` : timeOf(iso));

function fmtDuration(ms: number): string {
  const min = Math.round(ms / 60000);
  if (min < 60) return `${min} min`;
  if (min < 24 * 60) return `${Math.floor(min / 60)} h${min % 60 ? ` ${min % 60} min` : ""}`;
  const d = Math.floor(min / 1440);
  return `${d} d${Math.round((min % 1440) / 60) ? ` ${Math.round((min % 1440) / 60)} h` : ""}`;
}

/** "Stress test · 11/11 14:00 → 16:00 (2 h)"; atravessando a meia-noite mostra a data nos dois lados. */
export function eventLabel(e: SavedEvent): string {
  const sameDay = dateOf(e.start) === dateOf(e.end);
  const span = sameDay ? `${fmt(e.start, true)} → ${fmt(e.end, false)}` : `${fmt(e.start, true)} → ${fmt(e.end, true)}`;
  return `${e.name} · ${span} (${fmtDuration(Date.parse(e.end) - Date.parse(e.start))})`;
}
