// Detecta o erro de validação do próprio kube-apiserver quando alguém tenta editar
// campos imutáveis de um Pod já criado (ex: envFrom/SecretRef, volumes). Diferente de
// conflito de field manager (Helm/SSA) — nenhum --force-conflicts contorna isso, é uma
// regra de admissão do K8s: só spec.containers[*].image (e poucos outros campos) podem
// mudar num Pod existente.
const POD_IMMUTABLE_FIELDS_MARKER = "pod updates may not change fields other than";

export function isPodImmutableFieldsError(message: string): boolean {
  return message.includes(POD_IMMUTABLE_FIELDS_MARKER);
}

export const POD_IMMUTABLE_FIELDS_HINT =
  "O Kubernetes só permite alterar a imagem do container (e poucos outros campos, como tolerations) " +
  "em um Pod que já existe — os demais campos (env, envFrom, volumes, etc.) são imutáveis após a criação. " +
  "Para aplicar essa mudança, edite o Deployment/StatefulSet/DaemonSet responsável por esse Pod: " +
  "ao aplicar lá, o Kubernetes recria o Pod automaticamente com o valor corrigido.";
