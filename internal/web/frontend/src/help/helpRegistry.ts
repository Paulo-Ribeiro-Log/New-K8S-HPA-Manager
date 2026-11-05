// Registro das páginas de ajuda. O id é o mesmo id da aba (ver ToolsMenu/WorkloadMenu e o
// renderTabContent do Index); o texto fica em help/pages/<id>.md.
//
// Regra do projeto: nova aba ou ferramenta = item no menu + entrada aqui + página .md. O script
// scripts/check-help-coverage.mjs (make web-help-check) confere isso.

export type HelpGroup = "Geral" | "Principal" | "Workloads" | "Ferramentas";

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

export const HELP_GROUP_ORDER: HelpGroup[] = ["Geral", "Principal", "Workloads", "Ferramentas"];

export const HELP_IMPACT_LABEL: Record<HelpImpact, string> = {
  leitura: "Somente leitura",
  escrita: "Altera recursos",
  destrutiva: "Pode apagar recursos",
};

// Páginas "Geral" (id começa com `geral-`) não são de uma aba: explicam um padrão que várias abas
// compartilham e são alcançadas por link ([texto](ajuda:geral-...)) ou pelo índice.
export const HELP_ENTRIES: HelpEntry[] = [
  {
    id: "geral-abas-de-recurso",
    title: "Abas de recurso (padrão comum)",
    group: "Geral",
    impact: "escrita",
    summary: "O que ConfigMaps, Ingresses, Services, VPAs, DaemonSets e StatefulSets têm em comum: lista, editor YAML, diff, validar e aplicar.",
    keywords: ["yaml", "editor", "diff", "dry-run", "validar", "aplicar", "change history", "histórico", "excluir", "criar"],
  },
  {
    id: "configmaps",
    title: "ConfigMaps",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita, cria e exclui ConfigMaps, e mostra quais workloads os usam (ou se são órfãos).",
    keywords: ["configmap", "órfão", "uso", "envfrom", "volume", "checkpoint"],
  },
  {
    id: "ingresses",
    title: "Ingresses",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita, cria e exclui Ingresses; a busca também acha pelo host.",
    keywords: ["ingress", "host", "rota", "tls", "nginx"],
  },
  {
    id: "services",
    title: "Services",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita, cria e exclui Services.",
    keywords: ["service", "clusterip", "loadbalancer", "porta", "selector"],
  },
  {
    id: "vpas",
    title: "VPAs",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita e exclui VerticalPodAutoscalers existentes (não cria).",
    keywords: ["vpa", "vertical", "autoscaler", "recomendação", "requests"],
  },
  {
    id: "daemonsets",
    title: "DaemonSets",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista os DaemonSets e seus pods, edita o YAML, reinicia o rollout e exclui.",
    keywords: ["daemonset", "restart", "rollout", "pods", "node"],
  },
  {
    id: "statefulsets",
    title: "StatefulSets",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita o YAML, escala, reinicia o rollout e exclui StatefulSets.",
    keywords: ["statefulset", "scale", "réplicas", "restart", "rollout", "pvc"],
  },
  {
    id: "deployments",
    title: "Deployments",
    group: "Workloads",
    impact: "escrita",
    summary: "Lista, edita e opera Deployments: pods, escala, restart, rollback e análise preditiva.",
    keywords: ["pods", "rollout", "restart", "scale", "rollback", "replicas", "yaml", "spinnaker"],
  },
  {
    id: "secrets",
    title: "Secrets",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Lista, cria, edita e exclui Secrets, vê certificados TLS e controla a sincronização com o External Secrets.",
    keywords: ["secret", "base64", "tls", "certificado", "external secrets", "akv", "key vault", "resync", "pausar sincronização", "senha", "credencial"],
  },
  {
    id: "gateways",
    title: "Gateways",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Gerencia Gateway, HTTPRoute, GRPCRoute, TCPRoute e GatewayClass (Gateway API).",
    keywords: ["gateway api", "httproute", "grpcroute", "tcproute", "gatewayclass", "rota", "tráfego"],
  },
  {
    id: "pods",
    title: "Pods",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Tabela ao vivo de pods, com logs, describe, YAML, shell, SFTP, port-forward e restart, kill e exclusão.",
    keywords: ["pod", "logs", "describe", "shell", "exec", "sftp", "port-forward", "kill", "restart", "deletar", "crashloop"],
  },
  {
    id: "containers",
    title: "Containers",
    group: "Workloads",
    impact: "leitura",
    summary: "Tabela de containers (principais, init e efêmeros) com logs e detalhes de cada um.",
    keywords: ["container", "init", "efêmero", "logs", "imagem", "versão", "restarts"],
  },
  {
    id: "events",
    title: "Events",
    group: "Workloads",
    impact: "leitura",
    summary: "Eventos do Kubernetes por cluster e namespace, com filtro Normal/Warning e detalhes.",
    keywords: ["evento", "warning", "backoff", "failedscheduling", "kubectl get events"],
  },
  {
    id: "hpas",
    title: "HPAs",
    group: "Principal",
    impact: "escrita",
    summary: "Ajusta réplicas, metas de CPU e memória e requests/limits dos HPAs, com staging ou aplicação imediata.",
    keywords: ["hpa", "horizontalpodautoscaler", "réplicas", "min", "max", "target cpu", "requests", "limits", "rollout", "monitoramento", "staging"],
  },
  {
    id: "nodepools",
    title: "Node Pools / Node Groups",
    group: "Principal",
    impact: "destrutiva",
    summary: "Node pools (AKS) e node groups (EKS): tamanho, autoscaling, nodes, disco, pendentes e troca sequencial com cordon e drain.",
    keywords: ["node pool", "node group", "aks", "eks", "autoscaling", "spot", "sequencing", "cordon", "drain", "conntrack", "pendentes", "análise preditiva"],
  },
  {
    id: "namespaces",
    title: "Namespaces",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Uso por namespace, deployments, events, quotas, policies, RBAC; cria, edita e exclui namespaces.",
    keywords: ["namespace", "quota", "resourcequota", "networkpolicy", "rbac", "squad", "spot", "awx", "certificado tls", "deletar namespace"],
  },
  {
    id: "cronjobs",
    title: "CronJobs",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Lista, edita, suspende e dispara CronJobs; cria Jobs e CronJobs; apaga CronJobs e Jobs com proteções.",
    keywords: ["cronjob", "job", "trigger", "suspender", "agendamento", "schedule", "batch", "helm", "argo"],
  },
  {
    id: "nodes",
    title: "Nodes",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Nodes do cluster com uso, pools e taints; cordon, uncordon, drain, edição do YAML, shell e remoção.",
    keywords: ["node", "cordon", "uncordon", "drain", "evict", "taint", "node pool", "notready", "manutenção", "shell no node", "allocatable"],
  },
  {
    id: "staging",
    title: "Staging",
    group: "Principal",
    impact: "escrita",
    summary: "Fila de alterações pendentes de HPAs e Node Pools: revisar o de-para, aplicar em lote e guardar em sessões.",
    keywords: ["staging", "pendente", "aplicar todos", "sessão", "save session", "load session", "alterações", "lote"],
  },
  {
    id: "monitoring",
    title: "Monitoramento",
    group: "Ferramentas",
    impact: "leitura",
    summary: "Acompanha HPAs ao longo do tempo: CPU, memória, réplicas e alertas, a partir do Prometheus.",
    keywords: ["monitoramento", "monitoring", "hpa", "prometheus", "métricas", "cpu", "memória", "réplicas", "alertas", "engine"],
  },
  {
    id: "helm",
    title: "Helm",
    group: "Workloads",
    impact: "destrutiva",
    summary: "Releases Helm: values, histórico e manifest; install, upgrade, rollback e uninstall.",
    keywords: ["helm", "release", "chart", "values", "upgrade", "rollback", "uninstall", "install", "revisão", "manifest", "dry run"],
  },
  {
    id: "kafka-test",
    title: "Teste Kafka",
    group: "Ferramentas",
    impact: "escrita",
    summary: "Testa a conexão com Kafka ou Event Hub, lê e conta mensagens e, com confirmação, publica uma mensagem de teste.",
    keywords: ["kafka", "event hub", "kcat", "sasl", "tópico", "broker", "mensagens", "offset", "produce", "consume", "oauthbearer", "azure"],
  },
  {
    id: "net-discovery",
    title: "Descoberta de Rede",
    group: "Ferramentas",
    impact: "escrita",
    summary: "Traceroute TCP salto a salto com fingerprint do destino, ASN, nuvem e cruzamento com o Kubernetes; modos único, lote e monitor.",
    keywords: ["rede", "traceroute", "tcptraceroute", "salto", "hop", "nmap", "fingerprint", "asn", "porta", "mtls", "firewall", "rota", "dns reverso"],
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
