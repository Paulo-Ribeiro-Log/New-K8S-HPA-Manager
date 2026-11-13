// Relatórios que o operador salvou de propósito (botão "Salvar"). Lógica pura (testada em
// metricsSavedReports.test.ts); a chamada de rede fica em lib/api/client.ts.
import { REPORT_TZ, type ReportResult } from "./metricsReport";

export interface SavedReportMeta {
  id: number;
  name: string;
  created_at: string;
  window_start: string;
  window_end: string;
  apps: number;
  clusters: number;
  /** Tamanho do JSON do relatório, antes de comprimir. */
  size_bytes: number;
}

export interface SavedReport {
  meta: SavedReportMeta;
  result: ReportResult;
}

/** Limites do servidor (internal/storage/saved_reports_store.go): a tela avisa antes de tentar. */
export const MAX_SAVED_REPORTS = 50;
export const MAX_SAVED_NAME = 80;

const day = (iso: string) => new Date(iso).toLocaleDateString("pt-BR", { timeZone: REPORT_TZ, day: "2-digit", month: "2-digit" });
const hm = (iso: string) => new Date(iso).toLocaleTimeString("pt-BR", { timeZone: REPORT_TZ, hour: "2-digit", minute: "2-digit" });

/** Nome sugerido: "Relatório 11/11 14:00 → 16:00" (a janela do relatório, no fuso do relatório). */
export function defaultReportName(r: Pick<ReportResult, "window">): string {
  const { start, end } = r.window;
  return day(start) === day(end) ? `Relatório ${day(start)} ${hm(start)} → ${hm(end)}` : `Relatório ${day(start)} ${hm(start)} → ${day(end)} ${hm(end)}`;
}

/** "45 KB", "1,2 MB". */
export function fmtSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toLocaleString("pt-BR", { maximumFractionDigits: 1 })} MB`;
}

/** Uma linha de resumo para a lista: "13 aplicações · 2 clusters · 11/11 14:00 → 16:00 · 45 KB". */
export function describeSaved(m: SavedReportMeta): string {
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
  const span = day(m.window_start) === day(m.window_end) ? `${day(m.window_start)} ${hm(m.window_start)} → ${hm(m.window_end)}` : `${day(m.window_start)} ${hm(m.window_start)} → ${day(m.window_end)} ${hm(m.window_end)}`;
  return `${plural(m.apps, "aplicação", "aplicações")} · ${plural(m.clusters, "cluster", "clusters")} · ${span} · ${fmtSize(m.size_bytes)}`;
}

/** "salvo em 13/11 14:20". */
export function savedAt(m: SavedReportMeta): string {
  return `salvo em ${day(m.created_at)} ${hm(m.created_at)}`;
}

/** Nome aceito pelo servidor (não vazio, até 80 caracteres); devolve o motivo ou "" quando está bom. */
export function nameProblem(name: string): string {
  const n = name.trim();
  if (!n) return "Informe um nome.";
  if ([...n].length > MAX_SAVED_NAME) return `O nome pode ter até ${MAX_SAVED_NAME} caracteres.`;
  return "";
}
