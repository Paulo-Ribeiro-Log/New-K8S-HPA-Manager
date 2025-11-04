package scanner

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
)

// LoadClustersFromKubeconfig carrega lista de clusters do kubeconfig
func LoadClustersFromKubeconfig() ([]string, error) {
	// Tenta encontrar kubeconfig
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		// Default: ~/.kube/config
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	// Carrega kubeconfig
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	// Extrai nomes dos clusters
	clusters := make([]string, 0, len(config.Contexts))
	for contextName := range config.Contexts {
		clusters = append(clusters, contextName)
	}

	return clusters, nil
}

// FilterClustersByPattern filtra clusters por padrão (ex: "*-prd-admin", "*-hlg-admin")
func FilterClustersByPattern(clusters []string, pattern string) []string {
	if pattern == "" {
		return clusters
	}

	filtered := []string{}
	for _, cluster := range clusters {
		if matchPattern(cluster, pattern) {
			filtered = append(filtered, cluster)
		}
	}

	return filtered
}

// matchPattern verifica se string corresponde ao padrão (suporta * como wildcard)
func matchPattern(s, pattern string) bool {
	// Implementação simples de wildcard
	if pattern == "*" {
		return true
	}

	// Se não tem *, faz comparação exata
	if !contains(pattern, "*") {
		return s == pattern
	}

	// Suporte básico para * no início, fim ou meio
	if pattern[0] == '*' && pattern[len(pattern)-1] == '*' {
		// *texto*
		return contains(s, pattern[1:len(pattern)-1])
	} else if pattern[0] == '*' {
		// *sufixo
		suffix := pattern[1:]
		return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
	} else if pattern[len(pattern)-1] == '*' {
		// prefixo*
		prefix := pattern[:len(pattern)-1]
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}

	// Caso geral (sem wildcard ou formato complexo)
	return s == pattern
}

// contains verifica se string contém substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// LoadConfigForEnvironment carrega configuração para ambiente (PRD ou HLG)
func LoadConfigForEnvironment(env Environment) (*ScanConfig, error) {
	clusters, err := LoadClustersFromKubeconfig()
	if err != nil {
		return nil, err
	}

	config := DefaultScanConfig()
	config.Mode = ScanModeFull
	config.Environment = env

	// Filtra clusters por ambiente
	var pattern string
	if env == EnvironmentPRD {
		pattern = "*-prd-admin"
	} else {
		pattern = "*-hlg-admin"
	}

	filtered := FilterClustersByPattern(clusters, pattern)

	// Converte para targets
	config.Targets = make([]ScanTarget, len(filtered))
	for i, cluster := range filtered {
		config.Targets[i] = ScanTarget{
			Cluster:     cluster,
			Namespaces:  []string{}, // Todos
			Deployments: []string{}, // Todos
			HPAs:        []string{}, // Todos
		}
	}

	return config, nil
}
