package discovery

import "fmt"

// gmpHost é o host fixo do Google Cloud Monitoring API.
const gmpHost = "https://monitoring.googleapis.com"

// buildGMPURL constrói a URL Prometheus-compatible do Google Cloud Managed Service for Prometheus
// (GMP) a partir do Project ID embutido no context GKE (gke_<project>_<region>_<cluster>).
//
// Formato oficial do Google: expõe a API PromQL padrão (/api/v1/query, /api/v1/query_range, ...)
// sob esse prefixo — compatível com a forma como os clientes existentes concatenam
// endpoint.URL + "api/v1/...". Não suporta endpoints de introspecção de servidor Prometheus real
// (ex: /api/v1/status/config, /api/v1/alerts, /api/v1/rules) — GMP é só um backend de métricas
// consultável, não um Prometheus server completo.
//
// Retorna "" se `cluster` não for um context GKE — nesse caso o chamador cai no padrão
// viavarejo.com.br de sempre (buildPrometheusURL).
func buildGMPURL(cluster string) string {
	project := gkeProjectID(cluster)
	if project == "" {
		return ""
	}
	return fmt.Sprintf("%s/v1/projects/%s/location/global/prometheus/", gmpHost, project)
}
