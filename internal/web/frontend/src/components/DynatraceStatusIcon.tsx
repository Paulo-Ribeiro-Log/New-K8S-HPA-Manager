import { CheckCircle2, XCircle, AlertCircle } from "lucide-react";

export type DynatraceMonitoringStatus = "monitored" | "warning" | "unsupported";

interface DynatraceStatusIconProps {
  status: DynatraceMonitoringStatus;
  className?: string;
}

// Ícones estilo "selo de círculo" (check verde / X vermelho / exclamação âmbar). Mapeamento de
// severidade ajustado a pedido do usuário: "unsupported" (cluster sem Dynatrace configurado —
// não existe absolutamente nada pra checar) é um "não" definitivo, mais grave que "warning" (o
// cluster TEM Dynatrace, mas este pod específico não aparece na lista de monitorados — pode ser
// um problema pontual, ex: precisa de restart pro OneAgent injetar) — por isso "unsupported" usa
// vermelho (mais severo) e "warning" usa âmbar (atenção, não necessariamente um erro definitivo).
const ICON_BY_STATUS: Record<DynatraceMonitoringStatus, typeof CheckCircle2> = {
  monitored: CheckCircle2,
  warning: AlertCircle,
  unsupported: XCircle,
};

const COLOR_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "text-green-500 dark:text-green-400",
  warning: "text-amber-500 dark:text-amber-400",
  unsupported: "text-red-500 dark:text-red-400",
};

const TITLE_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "Monitorado pelo Dynatrace",
  warning: "Dynatrace configurado para este cluster, mas este pod não aparece monitorado no momento — pode ser um problema pontual (ex: precisa de restart pro OneAgent injetar, ou dado ainda não ingerido)",
  unsupported: "Dynatrace não configurado ou não aplicável para este cluster",
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
