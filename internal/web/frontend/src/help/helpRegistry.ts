// Registro das páginas de ajuda. O id é o mesmo id da aba (ver ToolsMenu/WorkloadMenu e o
// renderTabContent do Index); o texto fica em help/pages/<id>.md.
//
// Regra do projeto: nova aba ou ferramenta = item no menu + entrada aqui + página .md. O script
// scripts/check-help-coverage.mjs (make web-help-check) confere isso.

export type HelpGroup = "Principal" | "Workloads" | "Ferramentas";

/** O que a aba pode fazer: só ler, alterar recursos ou apagar/derrubar coisas. */
export type HelpImpact = "leitura" | "escrita" | "destrutiva";

export interface HelpEntry {
  id: string;
  title: string;
  group: HelpGroup;
  impact: HelpImpact;
  /** Uma frase, mostrada no índice. */
  summary: string;
  /** Palavras extras para a busca (sinônimos, nomes de recursos). */
  keywords?: string[];
}

export const HELP_GROUP_ORDER: HelpGroup[] = ["Principal", "Workloads", "Ferramentas"];

export const HELP_IMPACT_LABEL: Record<HelpImpact, string> = {
  leitura: "Somente leitura",
  escrita: "Altera recursos",
  destrutiva: "Pode apagar recursos",
};

export const HELP_ENTRIES: HelpEntry[] = [
  {
    id: "deployments",
    title: "Deployments",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita e opera Deployments: pods, escala, restart, rollback e análise preditiva.",
    keywords: ["pods", "rollout", "restart", "scale", "rollback", "replicas", "yaml", "spinnaker"],
  },
  {
    id: "finops",
    title: "FinOps",
    group: "Ferramentas",
    impact: "leitura",
    summary: "Custo, desperdício e oportunidades de economia por cluster, node pool e workload.",
    keywords: ["custo", "economia", "rightsizing", "showback", "node pool", "hpa", "prometheus", "dynatrace"],
  },
  {
    id: "app-removal",
    title: "Remoção de Aplicação",
    group: "Ferramentas",
    impact: "destrutiva",
    summary: "Descobre tudo o que pertence a uma aplicação e remove na ordem certa, com dry-run e verificação.",
    keywords: ["remover", "apagar", "deletar", "helm", "uninstall", "kustomize", "limpeza"],
  },
];

export function findHelpEntry(id: string | null | undefined): HelpEntry | undefined {
  return id ? HELP_ENTRIES.find((e) => e.id === id) : undefined;
}
