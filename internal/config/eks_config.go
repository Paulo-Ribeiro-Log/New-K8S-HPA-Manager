package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EKSClusterConfig representa a configuração de um cluster EKS no arquivo eks-clusters-config.json.
// Mantido separado de ClusterConfig (AKS) pois os campos não se sobrepõem.
type EKSClusterConfig struct {
	Name       string   `json:"clusterName"`
	AwsRegion  string   `json:"awsRegion"`
	AwsProfile string   `json:"awsProfile"`
	AccountID  string   `json:"awsAccountId,omitempty"`
	VpcID      string   `json:"vpcId,omitempty"`
	NodeGroups []string `json:"nodeGroups,omitempty"`
}

// eksConfigPath retorna o caminho padrão do arquivo eks-clusters-config.json
func eksConfigPath() string {
	return filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "eks-clusters-config.json")
}

// loadEKSClustersFromConfig carrega clusters EKS do arquivo eks-clusters-config.json.
func (k *KubeConfigManager) loadEKSClustersFromConfig() []EKSClusterConfig {
	data, err := os.ReadFile(eksConfigPath())
	if err != nil {
		return []EKSClusterConfig{}
	}
	var clusters []EKSClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		return []EKSClusterConfig{}
	}
	return clusters
}

// GetEKSClusterConfig retorna a configuração de um cluster EKS pelo nome.
// Busca primeiro em eks-clusters-config.json; se não encontrar, tenta o fallback
// de retrocompatibilidade no clusters-config.json legado (campos awsRegion/awsProfile).
func (k *KubeConfigManager) GetEKSClusterConfig(clusterName string) *EKSClusterConfig {
	normalized := strings.TrimSuffix(clusterName, "-admin")
	// clusterName costuma chegar como ARN completo (arn:aws:eks:REGION:ACCOUNT:cluster/NAME —
	// é o formato usado como context/cluster query param para EKS, ver detectSNATProvider),
	// mas Name no eks-clusters-config.json é sempre o nome curto (populado a partir de
	// `aws eks list-clusters` em eks_discovery.go). Sem extrair a parte após o último "/",
	// essa comparação nunca bate e a config é sempre reportada como "não encontrada" mesmo
	// existindo — mesmo padrão de extração já usado em resolveAWSProfile (kubeconfig.go).
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		normalized = normalized[idx+1:]
	}

	// 1. Fonte primária: eks-clusters-config.json
	for _, c := range k.loadEKSClustersFromConfig() {
		if strings.TrimSuffix(c.Name, "-admin") == normalized {
			return &c
		}
	}

	// 2. Retrocompatibilidade: clusters-config.json antigo com campos awsRegion/awsProfile
	for _, c := range k.loadClustersFromConfigCompat() {
		if strings.TrimSuffix(c.Name, "-admin") == normalized && c.AwsRegion != "" {
			return &EKSClusterConfig{
				Name:       c.Name,
				AwsRegion:  c.AwsRegion,
				AwsProfile: c.AwsProfile,
			}
		}
	}

	return nil
}

// SaveEKSClusterConfigs salva as configurações EKS descobertas em eks-clusters-config.json.
// Faz merge com configurações existentes (upsert por clusterName) e ordena alfabeticamente.
func (k *KubeConfigManager) SaveEKSClusterConfigs(configs []EKSClusterConfig, logFunc func(string)) error {
	path := eksConfigPath()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório: %w", err)
	}

	// Carregar existentes e fazer upsert
	existing := k.loadEKSClustersFromConfig()
	configMap := make(map[string]EKSClusterConfig, len(existing))
	for _, c := range existing {
		configMap[c.Name] = c
	}
	for _, c := range configs {
		configMap[c.Name] = c
	}

	// Converter para slice ordenado
	all := make([]EKSClusterConfig, 0, len(configMap))
	for _, c := range configMap {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("falha ao serializar EKS configs: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("falha ao salvar eks-clusters-config.json: %w", err)
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("💾 %d configurações EKS salvas em %s", len(all), path))
	}
	return nil
}

// clusterConfigCompat é usado apenas para leitura de retrocompatibilidade do
// clusters-config.json legado que misturava campos AKS e EKS.
type clusterConfigCompat struct {
	Name       string `json:"clusterName"`
	AwsRegion  string `json:"awsRegion,omitempty"`
	AwsProfile string `json:"awsProfile,omitempty"`
}

// loadClustersFromConfigCompat lê o clusters-config.json preservando os campos AWS legados.
// Usado apenas para migração — não usar em código novo.
func (k *KubeConfigManager) loadClustersFromConfigCompat() []clusterConfigCompat {
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")
	data, err := os.ReadFile(homeConfigPath)
	if err != nil {
		return nil
	}
	var clusters []clusterConfigCompat
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil
	}
	return clusters
}
