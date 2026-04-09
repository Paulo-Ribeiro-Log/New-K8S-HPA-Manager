/**
 * Utilitários para tabelas de monitoramento live (estilo kubectl)
 */

/**
 * Formata uma string ISO ou Date para o formato compacto do kubectl
 * ex: "2y266d", "3d5h", "45m30s", "97s"
 */
export function formatAge(isoOrDate: string | Date): string {
  const date = typeof isoOrDate === "string" ? new Date(isoOrDate) : isoOrDate;
  if (isNaN(date.getTime())) return "n/a";

  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const totalSeconds = Math.floor(diffMs / 1000);

  if (totalSeconds < 0) return "0s";
  if (totalSeconds < 60) return `${totalSeconds}s`;

  const minutes = Math.floor(totalSeconds / 60);
  if (minutes < 60) {
    const secs = totalSeconds % 60;
    return secs > 0 ? `${minutes}m${secs}s` : `${minutes}m`;
  }

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const mins = minutes % 60;
    return mins > 0 ? `${hours}h${mins}m` : `${hours}h`;
  }

  const days = Math.floor(hours / 24);
  if (days < 365) {
    const hrs = hours % 24;
    return hrs > 0 ? `${days}d${hrs}h` : `${days}d`;
  }

  const years = Math.floor(days / 365);
  const remainingDays = days % 365;
  return remainingDays > 0 ? `${years}y${remainingDays}d` : `${years}y`;
}

/**
 * Formata bytes para formato legível estilo kubectl (Ki, Mi, Gi)
 */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0";
  if (bytes < 1024) return `${bytes}`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)}Ki`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)}Mi`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)}Gi`;
}

/**
 * Formata millicores de CPU para formato kubectl
 * ex: 250 → "250m", 1500 → "1.5", 0 → "0m"
 */
export function formatMillicores(m: number): string {
  if (m <= 0) return "0m";
  if (m < 1000) return `${m}m`;
  return `${(m / 1000).toFixed(2).replace(/\.?0+$/, "")}`;
}

/** Converte string de CPU (ex: "300m", "1", "0.5") para millicores */
export function parseCpuToMillicores(s: string): number {
  if (!s || s === "0") return 0;
  if (s.endsWith("m")) return parseInt(s, 10);
  return Math.round(parseFloat(s) * 1000);
}

/** Converte string de memória (ex: "500Mi", "4Gi", "1024Ki") para bytes */
export function parseMemoryToBytes(s: string): number {
  if (!s || s === "0") return 0;
  if (s.endsWith("Ki")) return parseFloat(s) * 1024;
  if (s.endsWith("Mi")) return parseFloat(s) * 1024 * 1024;
  if (s.endsWith("Gi")) return parseFloat(s) * 1024 * 1024 * 1024;
  if (s.endsWith("Ti")) return parseFloat(s) * 1024 * 1024 * 1024 * 1024;
  if (s.endsWith("K") || s.endsWith("k")) return parseFloat(s) * 1000;
  if (s.endsWith("M")) return parseFloat(s) * 1000 * 1000;
  if (s.endsWith("G")) return parseFloat(s) * 1000 * 1000 * 1000;
  return parseFloat(s);
}

/**
 * Formata porcentagem ou retorna "n/a" se < 0
 */
export function formatPercent(v: number): string {
  if (v < 0) return "n/a";
  return `${Math.round(v)}%`;
}

/**
 * Retorna a classe Tailwind de cor para uma linha de pod baseada no status
 */
export function podRowColor(phase: string, reason?: string): string {
  const r = (reason ?? "").toLowerCase();
  const p = (phase ?? "").toLowerCase();

  if (r.includes("crashloop") || r.includes("error") || r.includes("oomkilled") || p === "failed") {
    return "text-red-400";
  }
  if (r === "terminating" || p === "terminating") return "text-purple-400";
  if (p === "running" && r === "notready") return "text-orange-400";
  if (p === "running") return "text-green-400";
  if (p === "pending") return "text-orange-400";
  if (p === "succeeded") return "text-gray-400";
  return "text-gray-300";
}

/**
 * Retorna a cor do dot de status para um pod
 */
export function podDotColor(phase: string, reason?: string): string {
  const r = (reason ?? "").toLowerCase();
  const p = (phase ?? "").toLowerCase();

  if (r.includes("crashloop") || r.includes("error") || r.includes("oomkilled") || p === "failed") {
    return "bg-red-500";
  }
  if (r === "terminating" || p === "terminating") return "bg-purple-500";
  if (p === "running" && r === "notready") return "bg-orange-500";
  if (p === "running") return "bg-green-500";
  if (p === "pending") return "bg-orange-500";
  if (p === "succeeded") return "bg-gray-500";
  return "bg-gray-600";
}
