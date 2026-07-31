package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// clusterPrometheusOverride é um subconjunto mínimo dos structs ClusterConfig/EKSClusterConfig/
// GKEClusterConfig (internal/config) — só os 2 campos necessários para resolver um override manual
// de URL do Prometheus a partir dos arquivos clusters-config.json/eks-clusters-config.json/
// gke-clusters-config.json.
//
// Duplicado aqui de propósito, em vez de importar internal/config: esse pacote fecharia um import
// cycle (internal/config → internal/cloudprovider/gcp → internal/ai → internal/collectors →
// internal/monitoring/client → internal/monitoring/discovery → internal/config). Os campos
// "clusterName"/"prometheusUrl" são lidos como JSON solto, então a duplicação não corre o risco de
// dessincronizar silenciosamente — um rename do campo em internal/config só pararia de bater aqui
// se a tag JSON mudasse também, o que já quebraria a compatibilidade do arquivo em disco.
type clusterPrometheusOverride struct {
	Name          string `json:"clusterName"`
	PrometheusURL string `json:"prometheusUrl"`
}

// getPrometheusURLOverride retorna a URL manual de Prometheus configurada para o cluster, ou "" se
// não houver override — nesse caso o chamador cai no padrão automático de descoberta
// (*.viavarejo.com.br), exatamente como sempre fez. Nenhuma instalação existente tem o campo
// "prometheusUrl" preenchido nesses arquivos, então para AKS/EKS já funcionando hoje o resultado é
// idêntico ao de antes desta função existir.
func getPrometheusURLOverride(cluster string) string {
	switch {
	case strings.HasPrefix(cluster, "gke_"):
		return overrideFromFile(configFilePath("gke-clusters-config.json"), gkeShortName(cluster), cluster)
	case strings.HasPrefix(cluster, "arn:aws:eks:"):
		return overrideFromFile(configFilePath("eks-clusters-config.json"), eksShortName(cluster), cluster)
	default:
		return overrideFromFile(configFilePath("clusters-config.json"), strings.TrimSuffix(cluster, "-admin"), cluster)
	}
}

func configFilePath(fileName string) string {
	return filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", fileName)
}

func overrideFromFile(path, shortName, fullName string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var entries []clusterPrometheusOverride
	if err := json.Unmarshal(data, &entries); err != nil {
		return ""
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name, "-admin")
		if (name == shortName || e.Name == fullName) && e.PrometheusURL != "" {
			return e.PrometheusURL
		}
	}
	return ""
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

// eksShortName extrai o nome curto do cluster de um ARN EKS completo, mesma convenção de
// internal/config.GetEKSClusterConfig.
func eksShortName(cluster string) string {
	normalized := strings.TrimSuffix(cluster, "-admin")
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		normalized = normalized[idx+1:]
	}
	return normalized
}
