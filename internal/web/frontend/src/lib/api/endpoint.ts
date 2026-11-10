// O apiClient já prefixa /api/v1 em toda requisição. Chamar apiClient.delete("/api/v1/x") vira
// "/api/v1/api/v1/x" (404) — foi exatamente o bug da exclusão em lote de ConfigMap/Ingress/Secret, que
// ainda engolia o erro. normalizeEndpoint remove o prefixo repetido (e avisa uma vez no console), para
// o erro de digitação não virar um 404 silencioso. Testada em endpoint.test.ts.

export const API_PREFIX = "/api/v1";

let warned = false;

export function normalizeEndpoint(endpoint: string): string {
  if (endpoint === API_PREFIX || endpoint.startsWith(API_PREFIX + "/") || endpoint.startsWith(API_PREFIX + "?")) {
    if (!warned) {
      warned = true;
      console.warn(`[apiClient] o endpoint "${endpoint}" já começa com ${API_PREFIX}, que o apiClient acrescenta sozinho; prefixo removido. Corrija a chamada.`);
    }
    return endpoint.slice(API_PREFIX.length) || "/";
  }
  return endpoint;
}

// só para os testes: permite observar o aviso mais de uma vez
export function _resetEndpointWarning() {
  warned = false;
}
