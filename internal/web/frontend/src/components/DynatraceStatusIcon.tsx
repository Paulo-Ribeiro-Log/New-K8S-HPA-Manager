import { CheckCircle2, XCircle, AlertCircle } from "lucide-react";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { DynatracePodCoverage } from "@/lib/api/types";

export type DynatraceMonitoringStatus = "monitored" | "warning" | "unsupported";

interface DynatraceStatusIconProps {
  status: DynatraceMonitoringStatus;
  className?: string;
  // Presente quando a checagem de monitoramento em si falhou (auth/rede no Dynatrace) — sobrescreve
  // o tooltip genérico de "warning" com o motivo real, em vez da mensagem enganosa de "pod não
  // aparece monitorado" (que sugere problema de instrumentação, não de autenticação).
  errorDetail?: string;
  // Detalhe de deep monitoring do pod (useDynatracePodCoverage) — acrescentado ao tooltip quando
  // disponível (só clusters com OneAgent clássico; ausente → tooltip de sempre).
  coverage?: DynatracePodCoverage;
}

const COVERAGE_STATUS: Record<string, { label: string; className: string }> = {
  Ativo: { label: "Ativo", className: "text-green-600 dark:text-green-400" },
  "Nao resolvido": { label: "Não resolvido", className: "text-amber-600 dark:text-amber-400" },
  "Sem servico": { label: "Sem serviço", className: "text-muted-foreground" },
};

function CoverageDetail({ coverage }: { coverage: DynatracePodCoverage }) {
  return (
    <div className="mt-2 pt-2 border-t space-y-1.5">
      <div className="text-muted-foreground">
        OneAgent <span className="font-mono text-foreground">{coverage.oneagent_version || "—"}</span>
      </div>
      <table className="w-full">
        <tbody>
          {coverage.processes.map((p, i) => {
            const st = COVERAGE_STATUS[p.deep_monitoring_status];
            return (
              <tr key={i} className="align-top">
                <td className="pr-3 py-0.5 break-all">{p.process_name}</td>
                <td className="pr-3 py-0.5 font-mono whitespace-nowrap">{p.technology || "—"}</td>
                <td className={`py-0.5 whitespace-nowrap ${st?.className ?? ""}`}>{st?.label ?? p.deep_monitoring_status}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
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
export function DynatraceStatusIcon({ status, className, errorDetail, coverage }: DynatraceStatusIconProps) {
  const Icon = ICON_BY_STATUS[status];
  const title = errorDetail ? `Falha ao checar monitoramento — ${errorDetail}` : TITLE_BY_STATUS[status];
  // Tooltip Radix (não o atributo `title` nativo, que não garante quebra de linha e ficava tudo
  // numa linha só com o detalhe de deep monitoring). Portal: as células das tabelas de pods têm
  // overflow-hidden e cortariam o conteúdo.
  return (
    <Tooltip delayDuration={200}>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center">
          <Icon className={`w-3.5 h-3.5 shrink-0 ${COLOR_BY_STATUS[status]} ${className ?? ""}`} />
        </span>
      </TooltipTrigger>
      <TooltipPrimitive.Portal>
        <TooltipContent className="max-w-md text-xs">
          <div>{title}</div>
          {!errorDetail && coverage && coverage.processes.length > 0 && <CoverageDetail coverage={coverage} />}
        </TooltipContent>
      </TooltipPrimitive.Portal>
    </Tooltip>
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
