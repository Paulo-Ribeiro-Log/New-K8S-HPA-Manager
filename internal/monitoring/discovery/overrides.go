package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// clusterPrometheusOverride é um subconjunto mínimo dos structs ClusterConfig/EKSClusterConfig/
// GKEClusterConfig (internal/config) — só os campos necessários para resolver um override manual
// de Prometheus (URL direta, ou alvo de port-forward pra um Prometheus só acessível dentro do
// cluster) a partir dos arquivos clusters-config.json/eks-clusters-config.json/
// gke-clusters-config.json.
//
// Duplicado aqui de propósito, em vez de importar internal/config: esse pacote fecharia um import
// cycle (internal/config → internal/cloudprovider/gcp → internal/ai → internal/collectors →
// internal/monitoring/client → internal/monitoring/discovery → internal/config). Os campos são
// lidos como JSON solto, então a duplicação não corre o risco de dessincronizar silenciosamente —
// um rename do campo em internal/config só pararia de bater aqui se a tag JSON mudasse também, o
// que já quebraria a compatibilidade do arquivo em disco.
type clusterPrometheusOverride struct {
	Name                         string `json:"clusterName"`
	PrometheusURL                string `json:"prometheusUrl"`
	PrometheusInClusterNamespace string `json:"prometheusInClusterNamespace"`
	PrometheusInClusterService   string `json:"prometheusInClusterService"`
	PrometheusInClusterPort      int    `json:"prometheusInClusterPort"`
}

// lookupOverrideEntry acha a entrada de config do cluster (se houver) no arquivo certo por
// provider, tentando nome curto e nome completo. Retorna zero-value se não encontrada — os
// getters abaixo (getPrometheusURLOverride/getPortForwardOverride) tratam campos vazios/zero como
// "sem override", caindo no comportamento automático de sempre.
func lookupOverrideEntry(cluster string) clusterPrometheusOverride {
	var path, shortName string
	switch {
	case strings.HasPrefix(cluster, "gke_"):
		path, shortName = configFilePath("gke-clusters-config.json"), gkeShortName(cluster)
	case strings.HasPrefix(cluster, "arn:aws:eks:"):
		path, shortName = configFilePath("eks-clusters-config.json"), eksShortName(cluster)
	default:
		path, shortName = configFilePath("clusters-config.json"), strings.TrimSuffix(cluster, "-admin")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return clusterPrometheusOverride{}
	}
	var entries []clusterPrometheusOverride
	if err := json.Unmarshal(data, &entries); err != nil {
		return clusterPrometheusOverride{}
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name, "-admin")
		if name == shortName || e.Name == cluster {
			return e
		}
	}
	return clusterPrometheusOverride{}
}

// getPrometheusURLOverride retorna a URL manual de Prometheus configurada para o cluster, ou "" se
// não houver override — nesse caso o chamador cai no padrão automático de descoberta
// (*.viavarejo.com.br), exatamente como sempre fez. Nenhuma instalação existente tem o campo
// "prometheusUrl" preenchido nesses arquivos, então para AKS/EKS já funcionando hoje o resultado é
// idêntico ao de antes desta função existir.
func getPrometheusURLOverride(cluster string) string {
	return lookupOverrideEntry(cluster).PrometheusURL
}

// getPortForwardOverride retorna o alvo de port-forward configurado manualmente pro cluster
// (Prometheus só acessível dentro do cluster, sem URL externa — ex: kube-prometheus-stack
// completo instalado num namespace, sem Ingress), ou PortForwardTarget zero-value se não
// configurado.
func getPortForwardOverride(cluster string) PortForwardTarget {
	e := lookupOverrideEntry(cluster)
	return PortForwardTarget{
		Namespace: e.PrometheusInClusterNamespace,
		Service:   e.PrometheusInClusterService,
		Port:      e.PrometheusInClusterPort,
	}
}

func configFilePath(fileName string) string {
	return filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", fileName)
}

// gkeShortName extrai o nome curto do cluster de um context GKE (gke_PROJECT_REGION_CLUSTER),
// mesma convenção de internal/config.splitGKEContext.
func gkeShortName(cluster string) string {
	parts := strings.Split(strings.TrimPrefix(cluster, "gke_"), "_")
	if len(parts) < 3 {
		return cluster
	}
	return parts[len(parts)-1]
}

// gkeProjectID extrai o Project ID de um context GKE (gke_PROJECT_REGION_CLUSTER) — mesma
// convenção de internal/config.splitGKEContext (não importado aqui pelo mesmo motivo do resto
// deste arquivo: evitar o import cycle cloudprovider/gcp → ... → monitoring/discovery). Google
// Cloud Project IDs nunca contêm "_" (só minúsculas/dígitos/hífen), então a divisão por "_" é
// inequívoca — as duas últimas partes são sempre region/cluster, tudo antes é o project ID.
// Retorna "" se `cluster` não tiver o formato de context GKE.
func gkeProjectID(cluster string) string {
	if !strings.HasPrefix(cluster, "gke_") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(cluster, "gke_"), "_")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-2], "_")
}

// eksShortName extrai o nome curto do cluster de um ARN EKS completo, mesma convenção de
// internal/config.GetEKSClusterConfig.
func eksShortName(cluster string) string {
	normalized := strings.TrimSuffix(cluster, "-admin")
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		normalized = normalized[idx+1:]
	}
	return normalized
}
