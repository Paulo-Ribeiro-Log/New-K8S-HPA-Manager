package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// ClusterConfig representa a configuração de um cluster no arquivo JSON
type ClusterConfig struct {
	Name          string `json:"clusterName"` // Mudado para "clusterName" para coincidir com o formato original
	ResourceGroup string `json:"resourceGroup"`
	Subscription  string `json:"subscription"`
}

// KubeConfigManager gerencia a configuração do Kubernetes
type KubeConfigManager struct {
	configPath  string
	config      *api.Config
	clients     map[string]kubernetes.Interface
	clientMutex sync.RWMutex // Protege acesso concorrente aos clients
}

// NewKubeConfigManager cria um novo gerenciador de kubeconfig
func NewKubeConfigManager(configPath string) (*KubeConfigManager, error) {
	config, err := clientcmd.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return &KubeConfigManager{
		configPath: configPath,
		config:     config,
		clients:    make(map[string]kubernetes.Interface),
	}, nil
}

// DiscoverClusters descobre clusters do kubeconfig que começam com "akspriv-" em ordem alfabética
func (k *KubeConfigManager) DiscoverClusters() []models.Cluster {
	var clusters []models.Cluster

	// Mapa para armazenar: cluster name -> context name
	clusterToContext := make(map[string]string)

	// Coletar clusters que começam com "akspriv-" e mapear para seus contexts
	for contextName, context := range k.config.Contexts {
		clusterName := context.Cluster
		if strings.HasPrefix(clusterName, "akspriv-") {
			// Armazenar mapeamento cluster -> context
			clusterToContext[clusterName] = contextName
		}
	}

	// Extrair nomes dos clusters e ordenar alfabeticamente
	var clusterNames []string
	for clusterName := range clusterToContext {
		clusterNames = append(clusterNames, clusterName)
	}
	sort.Strings(clusterNames)

	// Criar os objetos Cluster na ordem alfabética
	for _, clusterName := range clusterNames {
		cluster := models.Cluster{
			Name:    clusterName,
			Context: clusterToContext[clusterName], // Context name correto (ex: akspriv-xxx-admin)
			Status:  models.StatusUnknown,
		}
		clusters = append(clusters, cluster)
	}

	return clusters
}

// loadClustersFromConfig carrega clusters do arquivo clusters-config.json no diretório home
func (k *KubeConfigManager) loadClustersFromConfig() []ClusterConfig {
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")

	data, err := os.ReadFile(homeConfigPath)
	if err != nil {
		// Arquivo não existe ou não pode ser lido, retornar slice vazio
		return []ClusterConfig{}
	}

	var clusters []ClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		// Erro ao fazer parse do JSON, retornar slice vazio
		return []ClusterConfig{}
	}

	return clusters
}

// TestClusterConnection testa a conectividade com um cluster
func (k *KubeConfigManager) TestClusterConnection(ctx context.Context, clusterName string) models.ConnectionStatus {
	// Usar defer recover para capturar panics e converter em erro
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic recovered while testing cluster %s: %v\n", clusterName, r)
		}
	}()

	// Tentar criar cliente com tratamento gracioso de erros
	client, err := k.getClient(clusterName)
	if err != nil {
		// Log do erro para debug mas retorna status de erro sem panic
		fmt.Printf("Error creating client for cluster %s: %v\n", clusterName, err)
		return models.StatusError
	}

	// Criar contexto com timeout
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Tentar listar namespaces como teste de conectividade
	_, err = client.CoreV1().Namespaces().List(testCtx, metav1.ListOptions{Limit: 1})
	if err != nil {
		if testCtx.Err() == context.DeadlineExceeded {
			return models.StatusTimeout
		}
		return models.StatusError
	}

	return models.StatusConnected
}

// GetClient retorna um cliente Kubernetes para o cluster especificado
func (k *KubeConfigManager) GetClient(clusterName string) (kubernetes.Interface, error) {
	return k.getClient(clusterName)
}

// getClient cria ou retorna um cliente existente para o cluster
func (k *KubeConfigManager) getClient(clusterName string) (kubernetes.Interface, error) {
	// Primeiro, tentar ler o cliente existente com read lock (permite leituras concorrentes)
	k.clientMutex.RLock()
	if client, exists := k.clients[clusterName]; exists {
		k.clientMutex.RUnlock()
		return client, nil
	}
	k.clientMutex.RUnlock()

	// Cliente não existe - adquirir write lock para criação
	k.clientMutex.Lock()
	defer k.clientMutex.Unlock()

	// Double-check: outro goroutine pode ter criado o cliente enquanto esperávamos o lock
	if client, exists := k.clients[clusterName]; exists {
		return client, nil
	}

	// Verificar se o arquivo kubeconfig existe e é válido
	if k.configPath == "" {
		return nil, fmt.Errorf("kubeconfig path is empty")
	}

	// Verificar se o arquivo kubeconfig existe
	if _, err := os.Stat(k.configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kubeconfig file does not exist at path: %s", k.configPath)
	}

	// Criar configuração do cliente para o contexto específico
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: k.configPath}
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: clusterName}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	// Tentar obter configuração REST com tratamento de erro melhorado
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		// Tratar erros específicos de parsing YAML
		if strings.Contains(err.Error(), "yaml") || strings.Contains(err.Error(), "unmarshal") {
			return nil, fmt.Errorf("kubeconfig file has invalid YAML format for cluster %s: %w", clusterName, err)
		}
		return nil, fmt.Errorf("failed to create client config for %s: %w", clusterName, err)
	}

	// Configurar timeouts
	restConfig.Timeout = 30 * time.Second
	restConfig.QPS = 50
	restConfig.Burst = 100

	// Criar cliente Kubernetes
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client for %s: %w", clusterName, err)
	}

	// Armazenar cliente para reuso
	k.clients[clusterName] = client

	return client, nil
}

// ListContexts retorna todos os contextos disponíveis
func (k *KubeConfigManager) ListContexts() []string {
	var contexts []string
	for contextName := range k.config.Contexts {
		contexts = append(contexts, contextName)
	}
	return contexts
}

// GetCurrentContext retorna o contexto atual
func (k *KubeConfigManager) GetCurrentContext() string {
	return k.config.CurrentContext
}

// ValidateConfig valida a configuração do kubeconfig
func (k *KubeConfigManager) ValidateConfig() error {
	if k.config == nil {
		return fmt.Errorf("kubeconfig is not loaded")
	}

	if len(k.config.Contexts) == 0 {
		return fmt.Errorf("no contexts found in kubeconfig")
	}

	// Verificar se existem clusters akspriv-*
	hasAksprivClusters := false
	for contextName := range k.config.Contexts {
		if strings.HasPrefix(contextName, "akspriv-") {
			hasAksprivClusters = true
			break
		}
	}

	if !hasAksprivClusters {
		return fmt.Errorf("no akspriv-* clusters found in kubeconfig")
	}

	return nil
}

// GetClusterInfo retorna informações detalhadas sobre um cluster
func (k *KubeConfigManager) GetClusterInfo(clusterName string) (*ClusterInfo, error) {
	context, exists := k.config.Contexts[clusterName]
	if !exists {
		return nil, fmt.Errorf("context %s not found", clusterName)
	}

	cluster, exists := k.config.Clusters[context.Cluster]
	if !exists {
		return nil, fmt.Errorf("cluster %s not found", context.Cluster)
	}

	return &ClusterInfo{
		Name:      clusterName,
		Server:    cluster.Server,
		Context:   clusterName,
		Namespace: context.Namespace,
	}, nil
}

// ClusterInfo representa informações sobre um cluster
type ClusterInfo struct {
	Name      string
	Server    string
	Context   string
	Namespace string
}

// AutoDiscoverClusterConfig descobre automaticamente resource group e subscription a partir do kubeconfig e Azure CLI
func (k *KubeConfigManager) AutoDiscoverClusterConfig(clusterName string) (*ClusterConfig, error) {
	// 1. Extrair resource group do campo user no kubeconfig
	// Padrão: clusterAdmin_{RESOURCE_GROUP}_{CLUSTER_NAME}
	resourceGroup, err := k.extractResourceGroupFromKubeconfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract resource group: %w", err)
	}

	// 2. Descobrir subscription via Azure CLI
	subscription, err := k.discoverSubscriptionViaAzureCLI(clusterName, resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("failed to discover subscription: %w", err)
	}

	return &ClusterConfig{
		Name:           clusterName,
		ResourceGroup:  resourceGroup,
		Subscription:   subscription,
	}, nil
}

// extractResourceGroupFromKubeconfig extrai o resource group do nome do user no kubeconfig
// Padrão: clusterAdmin_{RESOURCE_GROUP}_{CLUSTER_NAME}
func (k *KubeConfigManager) extractResourceGroupFromKubeconfig(clusterName string) (string, error) {
	// Encontrar o context para o cluster
	var contextName string
	for name, ctx := range k.config.Contexts {
		if ctx.Cluster == clusterName {
			contextName = name
			break
		}
	}

	if contextName == "" {
		return "", fmt.Errorf("context not found for cluster %s", clusterName)
	}

	// Pegar o user name do context
	context := k.config.Contexts[contextName]
	userName := context.AuthInfo

	// Extrair resource group do user name
	// Formato: clusterAdmin_{RESOURCE_GROUP}_{CLUSTER_NAME}
	parts := strings.Split(userName, "_")
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected user name format: %s", userName)
	}

	// Resource group é o segundo elemento (index 1)
	resourceGroup := parts[1]
	return resourceGroup, nil
}

// discoverSubscriptionViaAzureCLI descobre a subscription buscando em todas as subscriptions disponíveis
func (k *KubeConfigManager) discoverSubscriptionViaAzureCLI(clusterName, resourceGroup string) (string, error) {
	// 1. Listar todas as subscriptions disponíveis
	cmd := exec.Command("az", "account", "list", "--query", "[].id", "-o", "tsv")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list subscriptions: %w", err)
	}

	subscriptions := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(subscriptions) == 0 {
		return "", fmt.Errorf("no subscriptions found")
	}

	// 2. Tentar encontrar o cluster em cada subscription
	for _, subscriptionID := range subscriptions {
		subscriptionID = strings.TrimSpace(subscriptionID)
		if subscriptionID == "" {
			continue
		}

		// Tentar az aks show com subscription específica
		cmd := exec.Command("az", "aks", "show",
			"--name", clusterName,
			"--resource-group", resourceGroup,
			"--subscription", subscriptionID,
			"--query", "id",
			"-o", "tsv")

		output, err := cmd.CombinedOutput()
		if err != nil {
			// Cluster não encontrado nesta subscription, tentar próxima
			continue
		}

		resourceID := strings.TrimSpace(string(output))
		if resourceID != "" {
			// Cluster encontrado! Extrair subscription do resource ID
			parts := strings.Split(resourceID, "/")
			for i, part := range parts {
				if part == "subscriptions" && i+1 < len(parts) {
					return parts[i+1], nil
				}
			}
		}
	}

	return "", fmt.Errorf("cluster not found in any subscription or no access")
}

// AutoDiscoverAllClusters descobre automaticamente configurações de todos os clusters do kubeconfig (paralelo)
func (k *KubeConfigManager) AutoDiscoverAllClusters(logFunc func(string)) ([]ClusterConfig, []error) {
	clusters := k.DiscoverClusters()

	if logFunc != nil {
		logFunc(fmt.Sprintf("🔍 Iniciando auto-descoberta paralela para %d clusters...", len(clusters)))
	}

	// Canais para resultados
	type result struct {
		index  int
		config *ClusterConfig
		err    error
	}

	resultChan := make(chan result, len(clusters))

	// Processar clusters em paralelo (máximo 10 simultaneamente)
	semaphore := make(chan struct{}, 10)

	for i, cluster := range clusters {
		go func(idx int, clusterName string) {
			semaphore <- struct{}{} // Adquirir slot
			defer func() { <-semaphore }() // Liberar slot

			config, err := k.AutoDiscoverClusterConfig(clusterName)
			resultChan <- result{index: idx, config: config, err: err}
		}(i, cluster.Name)
	}

	// Coletar resultados
	results := make([]result, len(clusters))
	for i := 0; i < len(clusters); i++ {
		res := <-resultChan
		results[res.index] = res

		// Mostrar progresso
		if logFunc != nil {
			if res.err != nil {
				logFunc(fmt.Sprintf("[%d/%d] ❌ %s: %v", i+1, len(clusters), clusters[res.index].Name, res.err))
			} else {
				logFunc(fmt.Sprintf("[%d/%d] ✅ %s - RG: %s, Sub: %s",
					i+1, len(clusters),
					clusters[res.index].Name,
					res.config.ResourceGroup,
					res.config.Subscription[:8]+"...")) // Mostrar apenas início do UUID
			}
		}
	}

	// Separar sucessos e erros
	var configs []ClusterConfig
	var errors []error

	for i, res := range results {
		if res.err != nil {
			errors = append(errors, fmt.Errorf("cluster %s: %w", clusters[i].Name, res.err))
		} else {
			configs = append(configs, *res.config)
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("📊 Resumo: ✅ %d sucesso | ❌ %d erros", len(configs), len(errors)))
	}

	return configs, errors
}

// SaveClusterConfigs salva as configurações descobertas no arquivo clusters-config.json
func (k *KubeConfigManager) SaveClusterConfigs(configs []ClusterConfig, logFunc func(string)) error {
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")

	// Criar diretório se não existir
	dir := filepath.Dir(homeConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Carregar configurações existentes
	existingConfigs := k.loadClustersFromConfig()

	// Criar mapa de configs existentes por nome
	configMap := make(map[string]ClusterConfig)
	for _, cfg := range existingConfigs {
		configMap[cfg.Name] = cfg
	}

	// Atualizar ou adicionar novas configs
	for _, cfg := range configs {
		configMap[cfg.Name] = cfg
	}

	// Converter mapa de volta para slice
	var allConfigs []ClusterConfig
	for _, cfg := range configMap {
		allConfigs = append(allConfigs, cfg)
	}

	// Ordenar alfabeticamente por nome
	sort.Slice(allConfigs, func(i, j int) bool {
		return allConfigs[i].Name < allConfigs[j].Name
	})

	// Serializar para JSON
	data, err := json.MarshalIndent(allConfigs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Salvar arquivo
	if err := os.WriteFile(homeConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("💾 Configurações salvas em: %s", homeConfigPath))
		logFunc(fmt.Sprintf("📝 Total de clusters: %d", len(allConfigs)))
	}

	return nil
}

// GetPodMetrics busca métricas de pods usando kubectl top
func (k *KubeConfigManager) GetPodMetrics(contextName, namespace, resourceName, workloadType string) (cpuUsage, memUsage string) {
	// Obter o client para este contexto
	clientset, err := k.GetClient(contextName)
	if err != nil {
		return "-", "-"
	}

	// Criar wrapper do client
	client := kubeclient.NewClient(clientset, contextName)

	// Buscar métricas
	return client.GetPodMetrics(namespace, resourceName, workloadType)
}
