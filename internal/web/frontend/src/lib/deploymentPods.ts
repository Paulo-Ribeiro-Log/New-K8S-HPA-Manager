import type { PodSummary } from "@/lib/api/types";

// Pods que pertencem a um Deployment, pelo dono real (OwnerReferences) e não pelo nome.
//
// O backend resolve `ownerWorkload` a partir do ReplicaSet dono do pod ("Deployment/<nome>"),
// tanto na listagem quanto no Watch. Casar por prefixo de nome (`<deploy>-`) ou pela label
// "app" misturava Deployments da mesma família (ex.: "api" e "api-worker"), e o fallback de
// "nada bateu, mostra tudo" despejava na tela todos os pods que só continham o texto no nome.
//
// Sem fallback de propósito: um Deployment com erro pode não ter pod nenhum (quota, admissão,
// ReplicaFailure), e a resposta correta é lista vazia, não pods de outro workload.

type PodOwner = Pick<PodSummary, "ownerWorkload">;

/** Identificador do dono como o backend o devolve em `ownerWorkload` e aceita em `GET /pods?owner=`. */
export function deploymentOwner(deploymentName: string): string {
  return `Deployment/${deploymentName}`;
}

export function podBelongsToDeployment(pod: PodOwner, deploymentName: string): boolean {
  const owner = pod.ownerWorkload;
  if (!owner) return false;
  if (owner === deploymentOwner(deploymentName)) return true;

  // Quando o sufixo do ReplicaSet não parece um hash, o backend devolve "ReplicaSet/<nome>".
  // O nome do ReplicaSet é "<deployment>-<hash>" e o hash nunca tem hífen: "api-worker-5d8f"
  // não pode ser tomado como ReplicaSet do Deployment "api".
  const prefix = `ReplicaSet/${deploymentName}-`;
  if (!owner.startsWith(prefix)) return false;
  const hash = owner.slice(prefix.length);
  return hash.length > 0 && !hash.includes("-");
}

export function filterPodsForDeployment<T extends PodOwner>(pods: T[], deploymentName: string): T[] {
  return pods.filter((p) => podBelongsToDeployment(p, deploymentName));
}
