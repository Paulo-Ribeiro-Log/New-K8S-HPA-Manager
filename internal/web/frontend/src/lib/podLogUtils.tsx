import type { ReactNode } from "react";

// Utilitários de log de pod compartilhados por PodQuickViewModal e AllPodsLogsModal: nível,
// coloração por severidade e destaque de busca.
// Exportados pra reuso em AllPodsLogsModal.tsx — mesmos recursos (filtro de nível, coloração por
// severidade) reaproveitados no modal de "logs de todos os pods", não uma versão reduzida.
export type LogLevel = "error" | "warn" | "info" | "debug";

export function getLogLevel(line: string): LogLevel | null {
  const l = line.toUpperCase();
  if (/\b(ERROR|FATAL|EXCEPTION|PANIC)\b/.test(l)) return "error";
  if (/\b(WARN|WARNING)\b/.test(l)) return "warn";
  if (/\b(INFO)\b/.test(l)) return "info";
  if (/\b(DEBUG|TRACE)\b/.test(l)) return "debug";
  return null;
}

export function logLineColor(line: string): string {
  const level = getLogLevel(line);
  if (level === "error") return "text-red-400";
  if (level === "warn") return "text-yellow-400";
  if (level === "debug") return "text-purple-400";
  if (level === "info") return "text-blue-400";
  if (/\s[45]\d{2}\s/.test(line)) return "text-orange-400";
  if (/\s2\d{2}\s/.test(line)) return "text-green-400";
  return "";
}

export const LOG_LEVEL_CONFIG: Record<LogLevel, { label: string; active: string; inactive: string }> = {
  error: { label: "ERR",   active: "bg-red-500/20 text-red-400 border-red-500/50",    inactive: "text-muted-foreground border-border hover:border-red-500/40 hover:text-red-400" },
  warn:  { label: "WARN",  active: "bg-yellow-500/20 text-yellow-400 border-yellow-500/50", inactive: "text-muted-foreground border-border hover:border-yellow-500/40 hover:text-yellow-400" },
  info:  { label: "INFO",  active: "bg-blue-500/20 text-blue-400 border-blue-500/50",  inactive: "text-muted-foreground border-border hover:border-blue-500/40 hover:text-blue-400" },
  debug: { label: "DEBUG", active: "bg-purple-500/20 text-purple-400 border-purple-500/50", inactive: "text-muted-foreground border-border hover:border-purple-500/40 hover:text-purple-400" },
};

// Exportadas pra reuso em AllPodsLogsModal.tsx (mesma lógica de destaque de busca em logs).
export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Destaca (case-insensitive) todas as ocorrências de `query` dentro de uma linha de log,
// espelhando o match de filterLogLines (mesmo .toLowerCase().includes) — só que aqui
// precisamos das posições exatas pra fatiar a string em spans normais + <mark>.
export function highlightMatches(line: string, query: string): ReactNode {
  const text = line || " ";
  const trimmed = query.trim();
  if (!trimmed) return text;
  const escaped = escapeRegExp(trimmed);
  const parts = text.split(new RegExp(`(${escaped})`, "gi"));
  if (parts.length === 1) return text;
  return parts.map((part, idx) =>
    part.toLowerCase() === trimmed.toLowerCase() ? (
      <mark key={idx} className="bg-yellow-400/80 text-black rounded-sm px-0.5">{part}</mark>
    ) : (
      <span key={idx}>{part}</span>
    )
  );
}
