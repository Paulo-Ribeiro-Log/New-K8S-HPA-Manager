// Status de monitoramento Dynatrace de um pod e seus rótulos (fora de DynatraceStatusIcon.tsx
// para esse arquivo exportar só componentes).

export type DynatraceMonitoringStatus = "monitored" | "warning" | "unsupported";

// Status de deep monitoring (dynatrace/coverage.go, coverageProcess.statusFor) — usado também
// pela aba Cobertura Dynatrace.
export const COVERAGE_STATUS: Record<string, { label: string; dot: string; text: string }> = {
  Ativo: { label: "Ativo", dot: "bg-green-500", text: "text-green-600 dark:text-green-400" },
  "Reinicio pendente": { label: "Reinício pendente", dot: "bg-blue-500", text: "text-blue-600 dark:text-blue-400" },
  Desativado: { label: "Desativado", dot: "bg-red-500", text: "text-red-600 dark:text-red-400" },
  "Nao resolvido": { label: "Não resolvido", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  "Sem servico": { label: "Sem serviço", dot: "bg-muted-foreground/40", text: "text-muted-foreground" },
};


// Rótulo em pt-BR usado no filtro/ordenação da coluna DT (PodMonitorTable.tsx) — os valores do
// tipo DynatraceMonitoringStatus em si (monitored/warning/unsupported) não são amigáveis pra UI.
export const DT_STATUS_LABEL: Record<DynatraceMonitoringStatus, string> = {
  monitored: "Monitorado",
  warning: "Não monitorado (verificar)",
  unsupported: "Não suportado",
};

// Ordem de prioridade pra ordenação da coluna DT — monitorado primeiro, depois os dois estados
// de atenção (warning antes de unsupported: um pod com cluster configurado que não aparece
// monitorado tende a ser mais acionável/urgente de investigar do que um cluster sem Dynatrace).
export const DT_STATUS_PRIORITY: Record<DynatraceMonitoringStatus, number> = {
  monitored: 0,
  warning: 1,
  unsupported: 2,
};


/** Deriva o status a partir do Set retornado por useDynatracePodStatus — usado nos dois painéis. */
export function resolveDynatraceStatus(
  clusterSupported: boolean,
  monitoredKeys: Set<string>,
  podKey: string
): DynatraceMonitoringStatus {
  if (!clusterSupported) return "unsupported";
  return monitoredKeys.has(podKey) ? "monitored" : "warning";
}
