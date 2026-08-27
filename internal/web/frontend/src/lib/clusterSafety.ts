import { deriveSpinnakerEnv } from "./spinnakerEnv";

// isProdClusterName — detecção AMPLA de cluster de produção, usada só pro destaque visual de
// segurança nos seletores de cluster (Header.tsx/ClusterSelectorForTab.tsx — cor laranja + filtro
// Todos/HLG/PRD, pedido explícito do usuário: "um analista sobrecarregado" não pode selecionar
// PRD sem perceber). A convenção estrita de sufixo "-prd" (deriveSpinnakerEnv) não pega clusters
// com nomenclatura diferente — ex: EKS documentado neste projeto como "asaplog-production", sem o
// sufixo "-prd". Pedido explícito do usuário: ampliar pra pegar qualquer variação de "produ*"
// (production/producao/produção/produtivo/etc.) em qualquer posição do nome, não só sufixo exato.
//
// Deliberadamente NÃO usada pra decisão de roteamento Spinnaker (Gate URL por ambiente,
// SpinnakerRolloutModal/DeploymentsTab) — lá a convenção precisa continuar estrita
// (deriveSpinnakerEnv sozinha): um falso positivo ali rotearia pro Gate errado. Aqui um falso
// positivo só acende um destaque visual a mais — sem risco real, e um falso negativo é o que
// importa evitar (por isso o critério mais permissivo).
export function isProdClusterName(cluster: string | undefined | null): boolean {
  if (!cluster) return false;
  if (deriveSpinnakerEnv(cluster) === "prd") return true;
  return /produ/i.test(cluster);
}

// isHlgClusterName — mesmo espírito do filtro, mas o usuário só pediu pra ampliar a detecção do
// lado PRD; HLG continua na convenção estrita de sufixo (sem pedido explícito de ampliar esse
// lado, e sem exemplo real de nome de cluster HLG fora dessa convenção neste projeto).
export function isHlgClusterName(cluster: string | undefined | null): boolean {
  return deriveSpinnakerEnv(cluster) === "hlg";
}
