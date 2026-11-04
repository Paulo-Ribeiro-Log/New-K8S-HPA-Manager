import yaml from "js-yaml";

// Documento YAML arbitrário (ConfigMap, Secret, Ingress, manifestos...): quem chama navega pelos
// campos que espera, então o retorno é dinâmico por natureza. É o único ponto que faz isso.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- árvore YAML arbitrária
export function parseYaml(text: string): any {
  return yaml.load(text);
}
