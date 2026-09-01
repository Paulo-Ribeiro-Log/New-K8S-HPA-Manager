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

// isHlgClusterName — usada pelo filtro Todos/HLG/PRD dos seletores (Header.tsx/
// ClusterSelectorForTab.tsx). Bug real corrigido: a versão anterior checava só a convenção
// estrita de sufixo "-hlg" (via deriveSpinnakerEnv) — qualquer cluster sem esse sufixo exato
// (ex: nomenclatura fora do padrão, clusters "sit"/"stg", EKS/GKE com nome livre) não batia
// nem em "-hlg" nem em `isProdClusterName`, e sumia do filtro "HLG" mesmo sendo, de fato, um
// cluster não-produtivo — ficava visível só em "Todos". Pedido explícito do usuário: "a lógica
// é tudo que não tiver prd/prod* é hlg" — corrigido invertendo o critério: HLG passa a ser
// "não é PRD" (usa a mesma detecção AMPLA de `isProdClusterName`), em vez de uma detecção
// própria e estrita. Mesma ressalva de `isProdClusterName`: deliberadamente NÃO usada pra
// roteamento Spinnaker (Gate URL por ambiente) — lá a convenção precisa continuar estrita
// (`deriveSpinnakerEnv` sozinha, sem passar por aqui).
export function isHlgClusterName(cluster: string | undefined | null): boolean {
  if (!cluster) return false;
  return !isProdClusterName(cluster);
}
