import { CheckCircle2, AlertTriangle, Ban } from "lucide-react";

export type DynatraceMonitoringStatus = "monitored" | "warning" | "unsupported";

interface DynatraceStatusIconProps {
  status: DynatraceMonitoringStatus;
  className?: string;
}

const ICON_BY_STATUS: Record<DynatraceMonitoringStatus, typeof CheckCircle2> = {
  monitored: CheckCircle2,
  warning: AlertTriangle,
  unsupported: Ban,
};

const COLOR_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "text-green-500 dark:text-green-400",
  warning: "text-amber-500 dark:text-amber-400",
  unsupported: "text-muted-foreground/50",
};

const TITLE_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "Monitorado pelo Dynatrace",
  warning: "Sem OneAgent detectado neste pod (Dynatrace disponível para o cluster)",
  unsupported: "Dynatrace não configurado/não aplicável para este cluster",
};

/** Ícone compacto de status de monitoramento Dynatrace, usado nos painéis esquerdo e direito da aba Pods. */
export function DynatraceStatusIcon({ status, className }: DynatraceStatusIconProps) {
  const Icon = ICON_BY_STATUS[status];
  return (
    <Icon
      className={`w-3.5 h-3.5 shrink-0 ${COLOR_BY_STATUS[status]} ${className ?? ""}`}
      title={TITLE_BY_STATUS[status]}
    />
  );
}

/** Deriva o status a partir do Set retornado por useDynatracePodStatus — usado nos dois painéis. */
export function resolveDynatraceStatus(
  clusterSupported: boolean,
  monitoredKeys: Set<string>,
  podKey: string
): DynatraceMonitoringStatus {
  if (!clusterSupported) return "unsupported";
  return monitoredKeys.has(podKey) ? "monitored" : "warning";
}
