// Deriva o ambiente Spinnaker ("hlg"/"prd") a partir do nome do cluster, pela mesma convenção
// de sufixo já usada em toda a app (ex: "akspriv-logreversa-hlg", "akspriv-ofertalogistica-prd")
// — remove o sufixo "-admin" primeiro (ver CLAUDE.md "Suffix -admin em Cluster Names").
// Retorna null quando não reconhece (ex: clusters "sit"/"stg" não têm Gate Spinnaker próprio
// confirmado — seção 1 do SPINNAKER-INTEGRATION-PLAN.md; "sit" roda dentro do Gate de hlg, mas
// não há como saber isso só pelo nome do cluster "sit", que nem segue essa convenção de sufixo).
export function deriveSpinnakerEnv(cluster: string | undefined | null): "hlg" | "prd" | null {
  if (!cluster) return null;
  const base = cluster.replace(/-admin$/, "");
  if (base.endsWith("-hlg")) return "hlg";
  if (base.endsWith("-prd")) return "prd";
  return null;
}
