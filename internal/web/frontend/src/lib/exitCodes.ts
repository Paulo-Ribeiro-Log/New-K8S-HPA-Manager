export type ExitCodeSeverity = "info" | "warning" | "critical";

export interface ExitCodeInfo {
  label: string;
  severity: ExitCodeSeverity;
}

// Significado dos exit codes mais comuns em containers Linux/K8s. Códigos >= 128 geralmente
// representam "128 + número do sinal" (convenção POSIX) — usado como fallback quando o código
// exato não está no mapa (ex: 128+9=137 SIGKILL, 128+15=143 SIGTERM, já mapeados explicitamente).
const EXIT_CODE_LABELS: Record<number, string> = {
  0: "Saída normal (sem erro)",
  1: "Erro genérico da aplicação",
  2: "Uso incorreto de comando do shell",
  126: "Comando encontrado mas não executável",
  127: "Comando não encontrado",
  128: "Argumento de exit inválido",
  130: "SIGINT — interrompido (Ctrl+C)",
  134: "SIGABRT — processo abortou (assert/panic)",
  137: "SIGKILL — kill forçado (geralmente OOMKilled ou probe/timeout matando o processo)",
  139: "SIGSEGV — falha de segmentação (acesso de memória inválido)",
  143: "SIGTERM — encerramento solicitado (rollout, scale down, preStop, ou kubelet pedindo shutdown)",
  255: "Exit status fora do intervalo válido (0-254)",
};

const SIGNAL_NAMES: Record<number, string> = {
  1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 6: "SIGABRT", 8: "SIGFPE",
  9: "SIGKILL", 11: "SIGSEGV", 13: "SIGPIPE", 15: "SIGTERM",
};

export function describeExitCode(code: number): ExitCodeInfo {
  const known = EXIT_CODE_LABELS[code];
  if (known) {
    const severity: ExitCodeSeverity = code === 0 ? "info" : code === 137 || code === 143 ? "critical" : "warning";
    return { label: known, severity };
  }
  if (code > 128 && code < 165) {
    const signal = code - 128;
    const name = SIGNAL_NAMES[signal] ?? `sinal ${signal}`;
    return { label: `${name} — processo encerrado pelo sinal ${signal}`, severity: "critical" };
  }
  if (code === 0) return { label: "Saída normal", severity: "info" };
  return { label: "Código de saída não padronizado — verifique a documentação da aplicação", severity: "warning" };
}
