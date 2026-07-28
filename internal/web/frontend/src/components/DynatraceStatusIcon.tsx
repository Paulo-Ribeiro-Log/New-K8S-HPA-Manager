import { CheckCircle2, XCircle, AlertCircle } from "lucide-react";

export type DynatraceMonitoringStatus = "monitored" | "warning" | "unsupported";

interface DynatraceStatusIconProps {
  status: DynatraceMonitoringStatus;
  className?: string;
}

// Ícones estilo "selo de circulo" (check verde / X vermelho / exclamação âmbar) — modelo pedido
// pelo usuário em vez do par CheckCircle2/AlertTriangle/Ban original da Fase 0.
const ICON_BY_STATUS: Record<DynatraceMonitoringStatus, typeof CheckCircle2> = {
  monitored: CheckCircle2,
  warning: XCircle,
  unsupported: AlertCircle,
};

const COLOR_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "text-green-500 dark:text-green-400",
  warning: "text-red-500 dark:text-red-400",
  unsupported: "text-amber-500 dark:text-amber-400",
};

const TITLE_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "Monitorado pelo Dynatrace",
  warning: "Sem OneAgent detectado neste pod (Dynatrace disponível para o cluster)",
  unsupported: "Dynatrace não configurado/não aplicável para este cluster",
};

// Rótulo em pt-BR usado no filtro/ordenação da coluna DT (PodMonitorTable.tsx) — os valores do
// tipo DynatraceMonitoringStatus em si (monitored/warning/unsupported) não são amigáveis pra UI.
export const DT_STATUS_LABEL: Record<DynatraceMonitoringStatus, string> = {
  monitored: "Monitorado",
  warning: "Não monitorado",
  unsupported: "Não suportado",
};

// Ordem de prioridade pra ordenação da coluna DT — monitorado primeiro, depois não monitorado,
// depois não suportado (mesma leitura visual verde→vermelho→âmbar dos ícones).
export const DT_STATUS_PRIORITY: Record<DynatraceMonitoringStatus, number> = {
  monitored: 0,
  warning: 1,
  unsupported: 2,
};

/** Ícone compacto de status de monitoramento Dynatrace, usado nos painéis esquerdo e direito da aba Pods. */
export function DynatraceStatusIcon({ status, className }: DynatraceStatusIconProps) {
  const Icon = ICON_BY_STATUS[status];
  // O tooltip precisa estar num <span> HTML, não na prop `title` do ícone lucide (que vira um
  // atributo `title` no <svg> raiz) — navegadores só mostram tooltip de SVG a partir de um
  // elemento <title> FILHO dentro do svg, não de um atributo `title` no próprio svg. Bug real
  // confirmado: o tooltip nunca aparecia em nenhum dos 3 estados (verde/vermelho/âmbar).
  return (
    <span title={TITLE_BY_STATUS[status]} className="inline-flex items-center">
      <Icon className={`w-3.5 h-3.5 shrink-0 ${COLOR_BY_STATUS[status]} ${className ?? ""}`} />
    </span>
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
