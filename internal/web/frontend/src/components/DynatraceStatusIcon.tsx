import { CheckCircle2, XCircle, AlertCircle, Cpu } from "lucide-react";
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

const COVERAGE_STATUS: Record<string, { label: string; dot: string; text: string }> = {
  Ativo: { label: "Ativo", dot: "bg-green-500", text: "text-green-600 dark:text-green-400" },
  "Nao resolvido": { label: "Não resolvido", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  "Sem servico": { label: "Sem serviço", dot: "bg-muted-foreground/40", text: "text-muted-foreground" },
};

// agentTechnologyType vem em maiúsculas com underscore (JAVA, DOTNET, NODE_JS...).
const TECHNOLOGY_LABEL: Record<string, string> = {
  JAVA: "Java", DOTNET: ".NET", NODE_JS: "Node.js", PYTHON: "Python", GO: "Go", PHP: "PHP",
  RUBY: "Ruby", NGINX: "NGINX", APACHE_HTTP_SERVER: "Apache", IIS: "IIS", ENVOY: "Envoy",
};

function technologyLabel(tech: string): string {
  if (!tech) return "";
  return TECHNOLOGY_LABEL[tech] ?? tech.toLowerCase().split("_").map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
}

function CoverageDetail({ coverage }: { coverage: DynatracePodCoverage }) {
  return (
    <>
      <div className="flex items-center gap-1.5 px-3 py-2 border-t bg-muted/30 text-[11px] text-muted-foreground">
        <Cpu className="w-3 h-3" />
        OneAgent
        <span className="ml-auto rounded bg-background/80 border px-1.5 py-0.5 font-mono text-[10px] text-foreground">
          {coverage.oneagent_version || "—"}
        </span>
      </div>
      <div className="px-3 pt-2 pb-2.5 border-t">
        <div className="mb-1.5 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          Processos · {coverage.processes.length}
        </div>
        <ul className="space-y-1.5">
          {coverage.processes.map((p, i) => {
            const st = COVERAGE_STATUS[p.deep_monitoring_status];
            return (
              <li key={i} className="flex items-start gap-2">
                <span className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${st?.dot ?? "bg-muted-foreground/40"}`} />
                <span className="min-w-0 flex-1 text-[11px] leading-snug break-words line-clamp-2">{p.process_name}</span>
                <span className="flex shrink-0 items-center gap-1.5">
                  {p.technology && (
                    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium">{technologyLabel(p.technology)}</span>
                  )}
                  <span className={`text-[10px] ${st?.text ?? "text-muted-foreground"}`}>{st?.label ?? p.deep_monitoring_status}</span>
                </span>
              </li>
            );
          })}
        </ul>
      </div>
    </>
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

// Título curto + descrição opcional do cabeçalho do tooltip.
const HEADLINE_BY_STATUS: Record<DynatraceMonitoringStatus, { title: string; description?: string }> = {
  monitored: { title: "Monitorado pelo Dynatrace" },
  warning: {
    title: "Pod não aparece monitorado",
    description: "O cluster tem Dynatrace, mas este pod não foi encontrado — pode precisar de restart para o OneAgent injetar, ou o dado ainda não foi ingerido.",
  },
  unsupported: { title: "Dynatrace indisponível", description: "Dynatrace não configurado ou não aplicável para este cluster." },
};

// Faixa superior do tooltip na cor do status — destaca o card do fundo da tela.
const ACCENT_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "border-t-green-500",
  warning: "border-t-amber-500",
  unsupported: "border-t-red-500",
};

const BADGE_BG_BY_STATUS: Record<DynatraceMonitoringStatus, string> = {
  monitored: "bg-green-500/15",
  warning: "bg-amber-500/15",
  unsupported: "bg-red-500/15",
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
  const headline = errorDetail
    ? { title: "Falha ao checar monitoramento", description: errorDetail }
    : HEADLINE_BY_STATUS[status];
  const showCoverage = !errorDetail && coverage && coverage.processes.length > 0;
  // Tooltip Radix (não o atributo `title` nativo, que não garante quebra de linha). Portal: as
  // células das tabelas de pods têm overflow-hidden e cortariam o conteúdo.
  return (
    <Tooltip delayDuration={200}>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center">
          <Icon className={`w-3.5 h-3.5 shrink-0 ${COLOR_BY_STATUS[status]} ${className ?? ""}`} />
        </span>
      </TooltipTrigger>
      <TooltipPrimitive.Portal>
        <TooltipContent
          className={`w-80 p-0 text-xs overflow-hidden border-foreground/20 border-t-2 shadow-2xl ring-1 ring-black/10 dark:ring-white/15 ${errorDetail ? "border-t-red-500" : ACCENT_BY_STATUS[status]}`}
        >
          <div className="flex items-start gap-2.5 px-3 py-2.5">
            <span className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${BADGE_BG_BY_STATUS[status]}`}>
              <Icon className={`w-3.5 h-3.5 ${COLOR_BY_STATUS[status]}`} />
            </span>
            <div className="min-w-0">
              <div className="font-semibold leading-tight">{headline.title}</div>
              {headline.description && (
                <p className={`mt-1 text-[11px] leading-snug text-muted-foreground break-words ${errorDetail ? "font-mono line-clamp-4" : ""}`}>
                  {headline.description}
                </p>
              )}
            </div>
          </div>
          {showCoverage && <CoverageDetail coverage={coverage} />}
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
