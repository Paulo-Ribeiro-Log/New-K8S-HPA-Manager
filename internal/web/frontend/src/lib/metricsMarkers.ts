// Marcadores pontuais do Tempo real ("Marcar agora"): um instante com um nome, por usuário, no servidor
// (o navegador guarda só uma cópia). Aparecem como linha vertical nos gráficos do modal e no resumo da
// sessão. Lógica pura, testada em metricsMarkers.test.ts.
import { REPORT_TZ } from "./metricsReport";

export interface Marker {
  id: string;
  name: string;
  /** RFC 3339 (UTC). */
  at: string;
}

export const MARKERS_KEY = "metrics_live_markers";
export const MAX_MARKERS = 200;
/** O dado do Prometheus e do Dynatrace some em ~14 dias; marcador de mais de 30 dias não aponta para nada. */
export const MARKER_MAX_AGE_MS = 30 * 24 * 3600e3;
export const MARKER_NAME_MAX = 60;

const valid = (m: unknown): m is Marker => {
  if (!m || typeof m !== "object") return false;
  const { id, name, at } = m as Record<string, unknown>;
  return typeof id === "string" && !!id && typeof name === "string" && !!name.trim() && typeof at === "string" && Number.isFinite(Date.parse(at));
};

const newest = (a: Marker, b: Marker) => Date.parse(b.at) - Date.parse(a.at) || a.name.localeCompare(b.name, "pt-BR");

/** Aceita o que veio do servidor ou do navegador e devolve só marcadores válidos e recentes, do mais novo ao mais antigo. */
export function parseMarkers(raw: unknown, now = Date.now()): Marker[] {
  let v = raw;
  if (typeof raw === "string") {
    try {
      v = JSON.parse(raw);
    } catch {
      return [];
    }
  }
  if (!Array.isArray(v)) return [];
  return v
    .filter(valid)
    .filter((m) => now - Date.parse(m.at) <= MARKER_MAX_AGE_MS)
    .map((m) => ({ id: m.id, name: m.name.trim().slice(0, MARKER_NAME_MAX), at: m.at }))
    .sort(newest)
    .slice(0, MAX_MARKERS);
}

let seq = 0;
/** Marca o instante dado (por padrão, agora). Nome vazio não grava. Vários marcadores com o mesmo nome são permitidos. */
export function addMarker(markers: Marker[], name: string, nowMs = Date.now()): Marker[] {
  const n = name.trim().slice(0, MARKER_NAME_MAX);
  if (!n) return markers;
  seq += 1;
  const m: Marker = { id: `${nowMs.toString(36)}${seq.toString(36)}${Math.random().toString(36).slice(2, 6)}`, name: n, at: new Date(nowMs).toISOString() };
  return [m, ...markers].sort(newest).slice(0, MAX_MARKERS);
}

export const removeMarker = (markers: Marker[], id: string): Marker[] => markers.filter((m) => m.id !== id);

/** Junta servidor e navegador sem perder marcador (pelo id). */
export function mergeMarkers(server: Marker[], local: Marker[]): Marker[] {
  const ids = new Set(server.map((m) => m.id));
  return [...server, ...local.filter((m) => !ids.has(m.id))].sort(newest).slice(0, MAX_MARKERS);
}

/** Marcadores dentro de um intervalo (epoch ms, inclusive), em ordem cronológica, com o instante em ms. */
export function markersInRange(markers: Marker[], fromMs: number, toMs: number): { id: string; name: string; t: number }[] {
  return markers
    .map((m) => ({ id: m.id, name: m.name, t: Date.parse(m.at) }))
    .filter((m) => m.t >= fromMs && m.t <= toMs)
    .sort((a, b) => a.t - b.t);
}

/** "14:12" (hora no fuso do relatório); com data quando não é de hoje. */
export function markerTime(at: string, nowMs = Date.now()): string {
  const d = new Date(at);
  const time = d.toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ, hour: "2-digit", minute: "2-digit" });
  const day = (x: Date) => x.toLocaleDateString("pt-BR", { timeZone: REPORT_TZ, day: "2-digit", month: "2-digit" });
  return day(d) === day(new Date(nowMs)) ? time : `${day(d)} ${time}`;
}

/** Nome curto para o rótulo do gráfico. */
export const shortName = (name: string, max = 24): string => (name.length > max ? `${name.slice(0, max - 1)}…` : name);
