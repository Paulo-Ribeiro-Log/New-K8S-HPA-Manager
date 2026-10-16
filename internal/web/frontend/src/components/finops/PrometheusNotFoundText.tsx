// Texto do banner "nenhum workload com dado real" quando todas as consultas ao Prometheus deram
// HTTP 404 (isPrometheusNotFound em helpers.ts) — compartilhado entre FinOpsTab e RightsizingTab.
export function PrometheusNotFoundText({ url }: { url?: string }) {
  return (
    <>
      Todas as consultas ao Prometheus responderam <strong>HTTP 404</strong>{url ? <> em <code className="text-xs">{url}</code></> : null}:
      o endereço responde, mas não serve a API do Prometheus — em geral o host padrão{" "}
      <code className="text-xs">prometheus-&lt;nome&gt;-&lt;ambiente&gt;.viavarejo.com.br</code> não foi publicado para este cluster, ou o
      Prometheus está em outro caminho. <strong>Não é falha transitória</strong>, reanalisar não resolve. Informe o endereço certo no campo{" "}
      <code className="text-xs">prometheusUrl</code> do cluster em <code className="text-xs">~/.k8s-hpa-manager/clusters-config.json</code>{" "}
      (ou o port-forward com <code className="text-xs">prometheusInClusterNamespace/Service/Port</code>) e reinicie o servidor.
    </>
  );
}
