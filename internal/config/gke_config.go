package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GKEClusterConfig representa a configuração de um cluster GKE no arquivo gke-clusters-config.json.
// Mantido separado de ClusterConfig (AKS) e EKSClusterConfig (EKS).
type GKEClusterConfig struct {
	Name          string `json:"clusterName"`
	ProjectID     string `json:"projectId"`
	Region        string `json:"region"`                  // us-central1 ou us-central1-a (zona)
	Network       string `json:"network,omitempty"`       // nome da VPC
	PrometheusURL string `json:"prometheusUrl,omitempty"` // override manual — GKE não segue o padrão de hostname viavarejo.com.br usado por AKS/EKS (ver GetPrometheusURLOverride)
}

// gkeConfigPath retorna o caminho padrão do arquivo gke-clusters-config.json
func gkeConfigPath() string {
	return filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "gke-clusters-config.json")
}

// loadGKEClustersFromConfig carrega clusters GKE do arquivo gke-clusters-config.json.
func (k *KubeConfigManager) loadGKEClustersFromConfig() []GKEClusterConfig {
	data, err := os.ReadFile(gkeConfigPath())
	if err != nil {
		return []GKEClusterConfig{}
	}
	var clusters []GKEClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		return []GKEClusterConfig{}
	}
	return clusters
}

// GetGKEClusterConfig retorna a configuração de um cluster GKE pelo nome do context.
// Aceita tanto o nome curto do cluster quanto o context GKE completo (gke_PROJECT_REGION_NAME).
func (k *KubeConfigManager) GetGKEClusterConfig(contextName string) *GKEClusterConfig {
	// Tentar extrair nome curto do context GKE (gke_PROJECT_REGION_NAME)
	shortName := parseGKEContextName(contextName)
	if shortName == "" {
		shortName = strings.TrimSuffix(contextName, "-admin")
	}

	for _, c := range k.loadGKEClustersFromConfig() {
		if c.Name == shortName || c.Name == contextName {
			return &c
		}
		// Comparar com nome curto extraído
		if parseGKEContextName(contextName) == c.Name {
			return &c
		}
	}

	// Fallback: parsear o context name diretamente
	if p, r, n := splitGKEContext(contextName); n != "" {
		return &GKEClusterConfig{Name: n, ProjectID: p, Region: r}
	}

	return nil
}

// SaveGKEClusterConfigs salva as configurações GKE descobertas em gke-clusters-config.json.
// Faz merge com configurações existentes (upsert por clusterName) e ordena alfabeticamente.
func (k *KubeConfigManager) SaveGKEClusterConfigs(configs []GKEClusterConfig, logFunc func(string)) error {
	path := gkeConfigPath()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório: %w", err)
	}

	existing := k.loadGKEClustersFromConfig()
	configMap := make(map[string]GKEClusterConfig, len(existing))
	for _, c := range existing {
		configMap[c.Name] = c
	}
	for _, c := range configs {
		configMap[c.Name] = c
	}

	all := make([]GKEClusterConfig, 0, len(configMap))
	for _, c := range configMap {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar GKE configs: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("falha ao salvar gke-clusters-config.json: %w", err)
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("💾 %d configurações GKE salvas em %s", len(all), path))
	}
	return nil
}

// parseGKEContextName extrai apenas o nome do cluster de um context GKE.
// Formato: gke_PROJECT_REGION_CLUSTER → retorna CLUSTER.
func parseGKEContextName(ctx string) string {
	_, _, name := splitGKEContext(ctx)
	return name
}

// splitGKEContext divide um context GKE no formato gke_PROJECT_REGION_CLUSTER.
// Retorna (project, region, cluster) ou ("", "", "") se não for GKE.
func splitGKEContext(ctx string) (project, region, cluster string) {
	if !strings.HasPrefix(ctx, "gke_") {
		return "", "", ""
	}
	// gke_PROJECT_REGION_CLUSTER — mas PROJECT pode ter underscores!
	// Convenção: últimas duas partes são REGION_CLUSTER; tudo antes é PROJECT.
	parts := strings.Split(strings.TrimPrefix(ctx, "gke_"), "_")
	if len(parts) < 3 {
		return "", "", ""
	}
	// Cluster = última parte, Region = penúltima, Project = resto
	cluster = parts[len(parts)-1]
	region = parts[len(parts)-2]
	project = strings.Join(parts[:len(parts)-2], "_")
	return project, region, cluster
}
