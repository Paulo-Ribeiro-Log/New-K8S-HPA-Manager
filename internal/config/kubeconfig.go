package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"k8s-hpa-manager/internal/cloudprovider"
	azureprovider "k8s-hpa-manager/internal/cloudprovider/azure"
	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"
	"k8s-hpa-manager/internal/history"
	kubeclient "k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
)

// ClusterConfig representa a configuração de um cluster AKS no arquivo clusters-config.json.
// Campos exclusivamente Azure — para EKS use EKSClusterConfig em eks_config.go.
type ClusterConfig struct {
	Name           string `json:"clusterName"`
	ResourceGroup  string `json:"resourceGroup"`
	Subscription   string `json:"subscription"`             // Nome legível ("PRD - ONLINE 2")
	SubscriptionID string `json:"subscriptionId,omitempty"` // UUID do Azure
}

// clientTTL define por quanto tempo um client inativo é mantido em memória
const clientTTL = 30 * time.Minute

// clientCleanupInterval define com qual frequência o cleanup roda
const clientCleanupInterval = 15 * time.Minute

// KubeConfigManager gerencia a configuração do Kubernetes
type KubeConfigManager struct {
	configPath      string
	config          *api.Config
	clients         map[string]kubernetes.Interface
	clientMutex     sync.RWMutex // Protege acesso concorrente aos clients
	metricsClients  map[string]*metricsclientset.Clientset
	metricsMutex    sync.RWMutex
	lastUsed        map[string]time.Time // Último acesso por cluster (clients + metricsClients)
	lastUsedMutex   sync.Mutex
	historyTracker  *history.HistoryTracker
}

// NewKubeConfigManager cria um novo gerenciador de kubeconfig
func NewKubeConfigManager(configPath string) (*KubeConfigManager, error) {
	config, err := clientcmd.LoadFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	km := &KubeConfigManager{
		configPath:     configPath,
		config:         config,
		clients:        make(map[string]kubernetes.Interface),
		metricsClients: make(map[string]*metricsclientset.Clientset),
		lastUsed:       make(map[string]time.Time),
		historyTracker: nil, // Será configurado via SetHistoryTracker
	}

	go km.clientCleanupLoop()

	return km, nil
}

// touchLastUsed atualiza o timestamp de último uso de um cluster
func (k *KubeConfigManager) touchLastUsed(clusterName string) {
	k.lastUsedMutex.Lock()
	k.lastUsed[clusterName] = time.Now()
	k.lastUsedMutex.Unlock()
}

// clientCleanupLoop remove clients inativos a cada clientCleanupInterval
func (k *KubeConfigManager) clientCleanupLoop() {
	ticker := time.NewTicker(clientCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		k.evictStaleClients()
	}
}

// evictStaleClients remove clients não usados há mais de clientTTL
func (k *KubeConfigManager) evictStaleClients() {
	cutoff := time.Now().Add(-clientTTL)

	k.lastUsedMutex.Lock()
	stale := make([]string, 0)
	for cluster, t := range k.lastUsed {
		if t.Before(cutoff) {
			stale = append(stale, cluster)
		}
	}
	for _, cluster := range stale {
		delete(k.lastUsed, cluster)
	}
	k.lastUsedMutex.Unlock()

	if len(stale) == 0 {
		return
	}

	k.clientMutex.Lock()
	for _, cluster := range stale {
		delete(k.clients, cluster)
	}
	k.clientMutex.Unlock()

	k.metricsMutex.Lock()
	for _, cluster := range stale {
		delete(k.metricsClients, cluster)
	}
	k.metricsMutex.Unlock()

	log.Info().
		Strs("clusters", stale).
		Dur("ttl", clientTTL).
		Msg("Evicted stale K8s clients from cache")
}

// ConfigPath retorna o caminho configurado do kubeconfig.
func (k *KubeConfigManager) ConfigPath() string {
	return k.configPath
}

// EnrichEKSError verifica se o erro é uma falha do exec credential provider do EKS
// (client-go rodando `aws eks get-token`) e enriquece a mensagem com "aws sso login --profile X"
// para que o frontend possa detectar e abrir o dialog de re-autenticação.
func (k *KubeConfigManager) EnrichEKSError(err error, clusterName string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	isExecError := strings.Contains(msg, "exec plugin") ||
		strings.Contains(msg, "couldn't get token") ||
		strings.Contains(msg, "exit status 255") ||
		strings.Contains(msg, "NoCredentialProviders") ||
		strings.Contains(msg, "ExpiredToken") ||
		strings.Contains(msg, "Partial credentials")
	if !isExecError {
		return err
	}
	profile := k.resolveAWSProfile(clusterName)
	if profile == "" {
		return err
	}
	return fmt.Errorf("%w. Execute: aws sso login --profile %s", err, profile)
}

// resolveAWSProfile retorna o perfil AWS para um contexto EKS, incluindo fallback
// por inferência do nome curto do cluster quando o kubeconfig não tem --profile explícito.
func (k *KubeConfigManager) resolveAWSProfile(contextName string) string {
	profile := k.getAWSProfileFromKubeconfig(contextName)
	if profile != "" {
		return profile
	}
	// Fallback: inferir do nome curto do cluster (ex: "asaplog-staging-naboo-admin" → "asaplog")
	serverURL := k.getServerURL(contextName)
	if DetectCloudProvider(serverURL) != CloudProviderEKS {
		return ""
	}
	shortName := contextName
	if idx := strings.LastIndex(contextName, "/"); idx >= 0 {
		shortName = contextName[idx+1:]
	}
	shortName = strings.TrimSuffix(shortName, "-admin")
	return inferAWSProfileFromEKSUserName(shortName)
}

// EvictClientsByAWSProfile remove do cache todos os clients K8s cujo perfil AWS
// corresponde ao informado. Deve ser chamado após login SSO bem-sucedido para
// forçar recriação com o novo token.
func (k *KubeConfigManager) EvictClientsByAWSProfile(profile string) {
	// Coletar contexts que correspondem ao perfil (sem locks).
	var toEvict []string
	k.clientMutex.RLock()
	for contextName := range k.clients {
		if k.resolveAWSProfile(contextName) == profile {
			toEvict = append(toEvict, contextName)
		}
	}
	k.clientMutex.RUnlock()

	if len(toEvict) == 0 {
		return
	}

	k.clientMutex.Lock()
	for _, name := range toEvict {
		delete(k.clients, name)
	}
	k.clientMutex.Unlock()

	k.metricsMutex.Lock()
	for _, name := range toEvict {
		delete(k.metricsClients, name)
	}
	k.metricsMutex.Unlock()
}

// SetHistoryTracker configura o historyTracker para audit logging
func (k *KubeConfigManager) SetHistoryTracker(tracker *history.HistoryTracker) {
	k.historyTracker = tracker
}

// GetK8sClient retorna um cliente wrapper *kubernetes.Client com historyTracker configurado
func (k *KubeConfigManager) GetK8sClient(clusterName string) (*kubeclient.Client, error) {
	clientset, err := k.GetClient(clusterName)
	if err != nil {
		return nil, err
	}

	client := kubeclient.NewClient(clientset, clusterName)

	// Configurar historyTracker se disponível
	if k.historyTracker != nil {
		client.SetHistoryTracker(k.historyTracker)
	}

	// Configurar Metrics Client (para GetNodeRawMetrics, GetNodesWithMetrics, etc.)
	if metricsClientIface, metricsErr := k.GetMetricsClient(clusterName); metricsErr == nil {
		if mc, ok := metricsClientIface.(*metricsclientset.Clientset); ok {
			client.SetMetricsClient(mc)
		}
	}

	return client, nil
}

// DiscoverClusters descobre todos os clusters do kubeconfig em ordem alfabética.
// Detecta automaticamente o cloud provider (AKS/EKS) via URL do API server.
func (k *KubeConfigManager) DiscoverClusters() []models.Cluster {
	// clusterName (kubeconfig) → context name
	clusterToContext := make(map[string]string)

	for contextName, ctx := range k.config.Contexts {
		clusterToContext[ctx.Cluster] = contextName
	}

	var clusterNames []string
	for clusterName := range clusterToContext {
		clusterNames = append(clusterNames, clusterName)
	}
	sort.Strings(clusterNames)

	var clusters []models.Cluster
	for _, clusterName := range clusterNames {
		serverURL := ""
		if c, ok := k.config.Clusters[clusterName]; ok {
			serverURL = c.Server
		}
		cloudProvider := DetectCloudProvider(serverURL)
		region := ""
		if cloudProvider == CloudProviderEKS {
			region = ExtractRegionFromEKSURL(serverURL)
		} else if cloudProvider == CloudProviderAKS {
			region = extractAKSRegion(serverURL)
		}

		displayName := clusterName
		awsProfile := ""
		if cloudProvider == CloudProviderEKS {
			// ARN arn:aws:eks:REGION:ACCOUNT:cluster/NAME → NAME
			if idx := strings.LastIndex(clusterName, "/"); idx >= 0 {
				displayName = clusterName[idx+1:]
			}
			// Expor o perfil AWS real (do kubeconfig) para que o frontend use sem inferência
			contextName := clusterToContext[clusterName]
			awsProfile = k.resolveAWSProfile(contextName)
		}

		clusters = append(clusters, models.Cluster{
			Name:          displayName,
			Context:       clusterToContext[clusterName],
			Status:        models.StatusUnknown,
			CloudProvider: cloudProvider,
			Region:        region,
			AWSProfile:    awsProfile,
		})
	}

	return clusters
}

// extractAKSRegion extrai a região de uma URL AKS.
// Exemplo: https://akspriv-abc.hcp.brazilsouth.azmk8s.io → "brazilsouth"
func extractAKSRegion(serverURL string) string {
	// Padrão: .hcp.<region>.azmk8s.io
	parts := strings.Split(serverURL, ".")
	for i, part := range parts {
		if part == "hcp" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
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

// TestClusterTCPConnection testa conectividade TCP pura com o API server do cluster.
// Não requer autenticação — apenas verifica se o endpoint está acessível via rede/VPN.
// Retorna true se a conexão TCP foi estabelecida dentro do timeout.
func (k *KubeConfigManager) TestClusterTCPConnection(clusterName string, timeout time.Duration) bool {
	serverURL := k.getServerURL(clusterName)
	if serverURL == "" {
		return false
	}

	u, err := url.Parse(serverURL)
	if err != nil {
		return false
	}

	host := u.Host
	// Garantir que a porta está presente (HTTPS default = 443)
	if host != "" && !strings.Contains(host, ":") {
		host = host + ":443"
	}
	if host == "" {
		return false
	}

	conn, err := net.DialTimeout("tcp", host, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// resolveContext encontra o nome real do contexto no kubeconfig a partir de um nome informado.
// Tenta: (1) nome exato, (2) sem sufixo -admin, (3) com sufixo -admin,
// (4) busca reversa por ARN EKS terminando em "/<name>" (suporta nome curto do cluster).
// Isso permite que o sistema funcione com AKS (com/sem -admin) e EKS
// (ARN completo ou nome curto), sem exigir normalização no frontend ou nos handlers.
func (k *KubeConfigManager) resolveContext(name string) string {
	if _, ok := k.config.Contexts[name]; ok {
		return name
	}
	// Tentar sem -admin (máquinas cujos contexts não têm o sufixo)
	if without := strings.TrimSuffix(name, "-admin"); without != name {
		if _, ok := k.config.Contexts[without]; ok {
			return without
		}
	}
	// Tentar com -admin (máquinas cujos contexts têm o sufixo mas o chamador não passou)
	if with := name + "-admin"; name != "" {
		if _, ok := k.config.Contexts[with]; ok {
			return with
		}
	}
	// Busca reversa para EKS: nome curto "my-cluster" → contexto ARN "arn:aws:eks:.../my-cluster"
	// Só chega aqui se os passos 1-3 falharam (nunca afeta AKS).
	suffix := "/" + name
	for contextName, ctx := range k.config.Contexts {
		if c, ok := k.config.Clusters[ctx.Cluster]; ok {
			if strings.HasSuffix(c.Server, ".eks.amazonaws.com") &&
				strings.HasSuffix(ctx.Cluster, suffix) {
				return contextName
			}
		}
	}
	return name // retorna original para que o erro eventual seja descritivo
}

// GetClient retorna um cliente Kubernetes para o cluster especificado
func (k *KubeConfigManager) GetClient(clusterName string) (kubernetes.Interface, error) {
	return k.getClient(clusterName)
}

// GetMetricsClient retorna um client para a API de métricas do cluster
func (k *KubeConfigManager) GetMetricsClient(clusterName string) (metricsclientset.Interface, error) {
	k.metricsMutex.RLock()
	if client, exists := k.metricsClients[clusterName]; exists {
		k.metricsMutex.RUnlock()
		k.touchLastUsed(clusterName)
		return client, nil
	}
	k.metricsMutex.RUnlock()

	restConfig, err := k.GetRestConfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for metrics of %s: %w", clusterName, err)
	}

	cfgCopy := rest.CopyConfig(restConfig)
	cfgCopy.Timeout = 15 * time.Second

	metricsClient, err := metricsclientset.NewForConfig(cfgCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics client for %s: %w", clusterName, err)
	}

	k.metricsMutex.Lock()
	defer k.metricsMutex.Unlock()
	if existing, exists := k.metricsClients[clusterName]; exists {
		return existing, nil
	}
	k.metricsClients[clusterName] = metricsClient
	k.touchLastUsed(clusterName)
	return metricsClient, nil
}

// GetRestConfig retorna a configuração REST para o cluster especificado
func (k *KubeConfigManager) GetRestConfig(clusterName string) (*rest.Config, error) {
	resolved := k.resolveContext(clusterName)

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
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: resolved}

	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	// Obter configuração REST
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		if strings.Contains(err.Error(), "yaml") || strings.Contains(err.Error(), "unmarshal") {
			return nil, fmt.Errorf("kubeconfig file has invalid YAML format for cluster %s: %w", resolved, err)
		}
		return nil, fmt.Errorf("failed to create client config for %s: %w", resolved, err)
	}

	// Configurar timeouts
	restConfig.Timeout = 30 * time.Second
	restConfig.QPS = 50
	restConfig.Burst = 100

	// Para clusters EKS, garantir que existe um ExecProvider funcional com `aws eks get-token`.
	//
	// Estratégia (por ordem de prioridade):
	// 1. Se o kubeconfig já tem ExecProvider → usar como está (comportamento igual ao K9s/kubectl).
	//    O usuário configurou o perfil correto via `aws eks update-kubeconfig --profile X`.
	// 2. Se só tem token estático (formato eks_<cluster>, expira em 15min) → substituir pelo
	//    nosso ExecProvider com o perfil inferido para renovação automática.
	// 3. Se não tem nem ExecProvider nem token → injetar nosso ExecProvider.
	//
	// Exceção: clusters com mTLS (CertData/KeyFile) não usam token AWS — ignorar.
	//
	// Nota: usar getServerURL(resolved) e não getServerURL(clusterName) para garantir que
	// clusters EKS no formato ARN (ex: "arn:aws:eks:.../asaplog-staging-mandalore") sejam
	// encontrados — o nome curto não existe como chave de contexto nem de cluster no kubeconfig.
	serverURL := k.getServerURL(resolved)
	if serverURL == "" {
		serverURL = k.getServerURL(clusterName)
	}
	if DetectCloudProvider(serverURL) == CloudProviderEKS && len(restConfig.CertData) == 0 && restConfig.KeyFile == "" {
		if restConfig.ExecProvider != nil {
			// Kubeconfig já tem exec provider configurado (ex: `aws eks update-kubeconfig --profile X`).
			// Respeitar como está — mesmo comportamento do K9s/kubectl.
		} else {
			// Sem exec provider: token estático expira em 15min ou kubeconfig sem auth.
			// Injetar nosso exec provider com perfil inferido para renovação automática.
			if exec := k.buildEKSExecProvider(resolved, serverURL, clusterName); exec != nil {
				restConfig.ExecProvider = exec
				restConfig.BearerToken = "" // Limpar token estático expirado
			}
		}
	}

	return restConfig, nil
}

// buildEKSExecProvider constrói um ExecConfig para `aws eks get-token` a partir
// das informações disponíveis no kubeconfig (contexto resolvido, URL do servidor).
func (k *KubeConfigManager) buildEKSExecProvider(resolved, serverURL, clusterName string) *api.ExecConfig {
	region := ExtractRegionFromEKSURL(serverURL)

	// Nome curto do cluster: extraído do cluster entry do contexto (pode ser ARN).
	shortName := ""
	if ctx, ok := k.config.Contexts[resolved]; ok {
		clusterEntry := ctx.Cluster
		if idx := strings.LastIndex(clusterEntry, "/"); idx >= 0 {
			shortName = clusterEntry[idx+1:]
		} else {
			shortName = clusterEntry
		}
	}
	// Fallback: usar o clusterName normalizado
	if shortName == "" {
		shortName = strings.TrimSuffix(strings.TrimSuffix(clusterName, "-admin"), "/")
		if idx := strings.LastIndex(shortName, "/"); idx >= 0 {
			shortName = shortName[idx+1:]
		}
	}

	profile := k.resolveAWSProfile(clusterName)

	if shortName == "" || region == "" {
		return nil
	}

	// --output json é obrigatório: sobrepõe o output=text que pode estar configurado
	// no perfil ~/.aws/config. Sem isso, client-go não consegue parsear o ExecCredential.
	args := []string{"eks", "get-token", "--cluster-name", shortName, "--region", region, "--output", "json"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}

	return &api.ExecConfig{
		APIVersion:      "client.authentication.k8s.io/v1beta1",
		Command:         "aws",
		Args:            args,
		InteractiveMode: api.NeverExecInteractiveMode,
	}
}

// getClient cria ou retorna um cliente existente para o cluster
func (k *KubeConfigManager) getClient(clusterName string) (kubernetes.Interface, error) {
	// Resolver o contexto real (suporta kubeconfigs com e sem sufixo -admin)
	resolved := k.resolveContext(clusterName)

	// Primeiro, tentar ler o cliente existente com read lock (permite leituras concorrentes)
	k.clientMutex.RLock()
	if client, exists := k.clients[resolved]; exists {
		k.clientMutex.RUnlock()
		k.touchLastUsed(resolved)
		return client, nil
	}
	k.clientMutex.RUnlock()

	// Cliente não existe - adquirir write lock para criação
	k.clientMutex.Lock()
	defer k.clientMutex.Unlock()

	// Double-check: outro goroutine pode ter criado o cliente enquanto esperávamos o lock
	if client, exists := k.clients[resolved]; exists {
		return client, nil
	}

	// Usar GetRestConfig para obter a configuração REST — já inclui injeção de
	// ExecProvider para EKS sem exec plugin no kubeconfig.
	restConfig, err := k.GetRestConfig(clusterName)
	if err != nil {
		return nil, err
	}

	// Criar cliente Kubernetes
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client for %s: %w", resolved, err)
	}

	// Armazenar cliente pelo nome resolvido para reuso
	k.clients[resolved] = client
	k.touchLastUsed(resolved)

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

// ValidateConfig valida a configuração do kubeconfig.
// Aceita qualquer cluster com contexto válido (AKS, EKS ou outro).
func (k *KubeConfigManager) ValidateConfig() error {
	if k.config == nil {
		return fmt.Errorf("kubeconfig is not loaded")
	}

	if len(k.config.Contexts) == 0 {
		return fmt.Errorf("no contexts found in kubeconfig")
	}

	return nil
}

// GetNodeGroupProvider retorna o NodeGroupProvider correto para o cluster.
// Detecta o cloud provider via URL do API server no kubeconfig.
// AKS → AzureNodeGroupProvider; EKS → AWSNodeGroupProvider; fallback por clusters-config.json.
func (k *KubeConfigManager) GetNodeGroupProvider(clusterName string) cloudprovider.NodeGroupProvider {
	normalizedName := strings.TrimSuffix(clusterName, "-admin")

	// Detectar cloud provider pela URL do API server
	serverURL := k.getServerURL(clusterName)
	cloudProvider := DetectCloudProvider(serverURL)

	configEntries := k.loadClustersFromConfig()

	switch cloudProvider {
	case CloudProviderAKS:
		for _, c := range configEntries {
			if strings.TrimSuffix(c.Name, "-admin") == normalizedName {
				return azureprovider.NewAzureNodeGroupProvider(c.Name, c.ResourceGroup, c.Subscription)
			}
		}
		// Kubeconfig diz AKS mas não está no clusters-config.json
		return azureprovider.NewAzureNodeGroupProvider(normalizedName, "", "")

	case CloudProviderEKS:
		// Base: extrair região e perfil do kubeconfig
		region := ExtractRegionFromEKSURL(serverURL)
		profile := k.getAWSProfileFromKubeconfig(clusterName)
		// eks-clusters-config.json tem prioridade (override explícito via autodiscover EKS)
		if eksConfig := k.GetEKSClusterConfig(clusterName); eksConfig != nil {
			if eksConfig.AwsRegion != "" {
				region = eksConfig.AwsRegion
			}
			if eksConfig.AwsProfile != "" {
				profile = eksConfig.AwsProfile
			}
		}
		return awsprovider.NewAWSNodeGroupProvider(normalizedName, region, profile)

	default:
		// URL não conclusiva — fallback por clusters-config.json
		for _, c := range configEntries {
			if strings.TrimSuffix(c.Name, "-admin") == normalizedName {
				return azureprovider.NewAzureNodeGroupProvider(c.Name, c.ResourceGroup, c.Subscription)
			}
		}
		return awsprovider.NewAWSNodeGroupProvider(normalizedName, "", "")
	}
}

// getServerURL retorna a URL do API server de um cluster/context pelo nome.
// GetServerURL expõe a URL do API server de um cluster (usado por handlers externos).
func (k *KubeConfigManager) GetServerURL(name string) string { return k.getServerURL(name) }

func (k *KubeConfigManager) getServerURL(name string) string {
	// Tenta como context name
	if ctx, ok := k.config.Contexts[name]; ok {
		if c, ok := k.config.Clusters[ctx.Cluster]; ok {
			return c.Server
		}
	}
	// Tenta adicionando -admin
	if ctx, ok := k.config.Contexts[name+"-admin"]; ok {
		if c, ok := k.config.Clusters[ctx.Cluster]; ok {
			return c.Server
		}
	}
	// Tenta como cluster name direto
	if c, ok := k.config.Clusters[name]; ok {
		return c.Server
	}
	return ""
}

// getAWSProfileFromKubeconfig extrai o perfil AWS do contexto EKS no kubeconfig.
// Procura em três fontes (em ordem de prioridade):
//  1. env: [{name: AWS_PROFILE, value: "..."}] no exec plugin (formato antigo)
//  2. args: [..., "--profile", "<value>", ...] no exec plugin (formato antigo)
//  3. user name com prefixo "eks_" → infere perfil pelo nome base (formato novo, sem exec plugin)
func (k *KubeConfigManager) getAWSProfileFromKubeconfig(clusterName string) string {
	resolvedCtx := k.resolveContext(clusterName)
	ctx, ok := k.config.Contexts[resolvedCtx]
	if !ok {
		return ""
	}
	authInfo, ok := k.config.AuthInfos[ctx.AuthInfo]
	if !ok || authInfo == nil {
		return ""
	}

	if authInfo.Exec != nil {
		// 1. Verificar variáveis de ambiente do exec plugin (AWS_PROFILE)
		for _, env := range authInfo.Exec.Env {
			if env.Name == "AWS_PROFILE" && env.Value != "" {
				return env.Value
			}
		}
		// 2. Verificar flag --profile nos args
		args := authInfo.Exec.Args
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "--profile" {
				return args[i+1]
			}
		}
	}

	// 3. Novo formato: user name "eks_<cluster-name>" sem exec plugin.
	// Infere o perfil AWS removendo sufixos de ambiente conhecidos do nome do cluster.
	// Exemplo: "eks_asaplog-preprod" → "asaplog", "eks_asaplog-staging-alderaan" → "asaplog"
	if strings.HasPrefix(ctx.AuthInfo, "eks_") {
		return inferAWSProfileFromEKSUserName(ctx.AuthInfo)
	}

	return ""
}

// inferAWSProfileFromEKSUserName extrai o perfil AWS do nome de usuário no formato "eks_<cluster>".
// Remove sufixos de ambiente conhecidos para obter o nome da conta/org.
func inferAWSProfileFromEKSUserName(userName string) string {
	name := strings.TrimPrefix(userName, "eks_")
	// Sufixos de ambiente: -staging-<qualquer-coisa>, -preprod, -production, -debezium
	for _, suffix := range []string{"-staging-", "-preprod", "-production", "-debezium"} {
		if idx := strings.Index(name, suffix); idx > 0 {
			return name[:idx]
		}
	}
	// Sem sufixo reconhecível — usa o nome completo como perfil (ex: "asapops")
	return name
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

// AutoDiscoverClusterConfig descobre resource group e subscription de um cluster AKS.
// subscriptions deve ser pré-carregado via loadAllAzureSubscriptions (evita N chamadas redundantes).
func (k *KubeConfigManager) AutoDiscoverClusterConfig(ctx context.Context, clusterName string, subscriptions []string) (*ClusterConfig, error) {
	resourceGroup, err := k.extractResourceGroupFromKubeconfig(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract resource group: %w", err)
	}

	subscriptionID, subscriptionName, err := k.discoverSubscriptionViaAzureCLI(ctx, clusterName, resourceGroup, subscriptions)
	if err != nil {
		return nil, fmt.Errorf("failed to discover subscription: %w", err)
	}

	return &ClusterConfig{
		Name:           clusterName,
		ResourceGroup:  resourceGroup,
		Subscription:   subscriptionName,
		SubscriptionID: subscriptionID,
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
	// O RG pode conter underscores (ex: clusterAdmin_rg_aks_prod_mycluster → rg_aks_prod)
	firstUnderscore := strings.Index(userName, "_")
	if firstUnderscore < 0 {
		return "", fmt.Errorf("unexpected user name format: %s", userName)
	}
	withoutPrefix := userName[firstUnderscore+1:] // "RESOURCE_GROUP_CLUSTER_NAME"

	// Remover o sufixo "_CLUSTER_NAME" para obter o RG completo
	suffix := "_" + clusterName
	if strings.HasSuffix(withoutPrefix, suffix) {
		return strings.TrimSuffix(withoutPrefix, suffix), nil
	}

	// Fallback: comportamento original (segundo segmento)
	parts := strings.Split(userName, "_")
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected user name format: %s", userName)
	}
	return parts[1], nil
}

// loadAllAzureSubscriptions carrega a lista de subscription IDs disponíveis via Azure CLI.
// Chamada uma única vez em AutoDiscoverAllClusters e compartilhada entre todos os clusters,
// eliminando N chamadas redundantes a "az account list".
// Se o token estiver expirado, tenta renovar automaticamente via az login --use-device-code.
func (k *KubeConfigManager) loadAllAzureSubscriptions(ctx context.Context, logFunc func(string)) ([]string, error) {
	// Validar token antes de qualquer operação
	tokCtx, tokCancel := context.WithTimeout(ctx, 15*time.Second)
	defer tokCancel()
	if err := exec.CommandContext(tokCtx, "az", "account", "get-access-token", "--only-show-errors").Run(); err != nil {
		if logFunc != nil {
			logFunc("[AKS] 🔑 Token Azure expirado — iniciando az login --use-device-code...")
			logFunc("[AKS] 💡 Abra https://microsoft.com/devicelogin e insira o código exibido abaixo:")
		}
		// Usar contexto separado sem timeout (az login --use-device-code aguarda o usuário)
		loginCmd := exec.CommandContext(context.Background(), "az", "login", "--use-device-code", "--only-show-errors")
		loginCmd.Stdout = os.Stdout
		loginCmd.Stderr = os.Stderr
		if loginErr := loginCmd.Run(); loginErr != nil {
			return nil, fmt.Errorf("az login falhou: %w — execute 'az login' manualmente e tente novamente", loginErr)
		}
		if logFunc != nil {
			logFunc("[AKS] ✅ Login Azure concluído — continuando discovery...")
		}
	}

	listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
	defer listCancel()
	out, err := exec.CommandContext(listCtx, "az", "account", "list",
		"--query", "[].id", "-o", "tsv", "--only-show-errors").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions (erro 401 indica token expirado - execute 'az login'): %w\nOutput: %s", err, string(out))
	}

	var valid []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			valid = append(valid, s)
		}
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no valid subscriptions found")
	}
	return valid, nil
}

// discoverSubscriptionViaAzureCLI descobre a subscription de um cluster AKS buscando na lista
// pré-carregada de subscriptions (paralelo controlado, semáforo cap. 15).
// Retorna (subscriptionID, subscriptionName, error).
func (k *KubeConfigManager) discoverSubscriptionViaAzureCLI(ctx context.Context, clusterName, resourceGroup string, validSubscriptions []string) (string, string, error) {
	type result struct {
		subscriptionID string
		resourceID     string
		err            error
	}

	resultChan := make(chan result, len(validSubscriptions))
	semaphore := make(chan struct{}, 10) // cap. 10 — subscriptions por cluster (inner loop)
	var wg sync.WaitGroup

	subCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	for _, subscriptionID := range validSubscriptions {
		wg.Add(1)
		go func(subID string) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-subCtx.Done():
				resultChan <- result{subscriptionID: subID, err: subCtx.Err()}
				return
			}

			cmdCtx, cmdCancel := context.WithTimeout(subCtx, 20*time.Second) // reduzido 30s → 20s
			defer cmdCancel()

			cmd := exec.CommandContext(cmdCtx, "az", "aks", "show",
				"--name", clusterName,
				"--resource-group", resourceGroup,
				"--subscription", subID,
				"--query", "id",
				"-o", "tsv",
				"--only-show-errors")

			output, err := cmd.CombinedOutput()
			if err != nil {
				if strings.Contains(strings.ToLower(string(output)), "401") ||
					strings.Contains(strings.ToLower(string(output)), "unauthorized") ||
					strings.Contains(strings.ToLower(string(output)), "authentication") {
					resultChan <- result{subscriptionID: subID, err: fmt.Errorf("erro de autenticação (401) - token Azure expirado. Execute 'az login' novamente")}
					return
				}
				resultChan <- result{subscriptionID: subID, err: err}
				return
			}

			resultChan <- result{subscriptionID: subID, resourceID: strings.TrimSpace(string(output))}
		}(subscriptionID)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var foundSubscriptionID string
	for res := range resultChan {
		if foundSubscriptionID == "" && res.err == nil && res.resourceID != "" {
			parts := strings.Split(res.resourceID, "/")
			for j, part := range parts {
				if part == "subscriptions" && j+1 < len(parts) {
					foundSubscriptionID = parts[j+1]
					cancel()
					break
				}
			}
		}
	}

	if foundSubscriptionID == "" {
		return "", "", fmt.Errorf("cluster not found in any subscription or no access")
	}

	nameCtx, nameCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer nameCancel()
	nameOut, nameErr := exec.CommandContext(nameCtx, "az", "account", "show",
		"--subscription", foundSubscriptionID,
		"--query", "name", "-o", "tsv", "--only-show-errors").Output()
	if nameErr == nil {
		if name := strings.TrimSpace(string(nameOut)); name != "" {
			return foundSubscriptionID, name, nil
		}
	}

	return foundSubscriptionID, foundSubscriptionID, nil
}

// AutoDiscoverAllClusters descobre automaticamente configurações de todos os clusters AKS do kubeconfig.
// Pré-carrega a lista de subscriptions Azure uma única vez e a compartilha entre todos os clusters,
// eliminando N chamadas redundantes a "az account list". Semáforo aumentado de 3 → 10 clusters simultâneos.
func (k *KubeConfigManager) AutoDiscoverAllClusters(logFunc func(string)) ([]ClusterConfig, []error) {
	clusters := k.DiscoverClusters()

	// Filtrar apenas clusters AKS (EKS é tratado em AutoDiscoverEKSClusters)
	var aksClusters []models.Cluster
	for _, c := range clusters {
		if c.CloudProvider != CloudProviderEKS {
			aksClusters = append(aksClusters, c)
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 🔍 Iniciando auto-descoberta paralela para %d clusters AKS...", len(aksClusters)))
	}

	if len(aksClusters) == 0 {
		return nil, nil
	}

	// Timeout global de 10 minutos (aumentado de 5)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Pré-carregar subscriptions uma única vez — elimina N chamadas redundantes
	if logFunc != nil {
		logFunc("[AKS] 📋 Carregando subscriptions Azure...")
	}
	subscriptions, err := k.loadAllAzureSubscriptions(ctx, logFunc)
	if err != nil {
		return nil, []error{err}
	}
	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 📋 %d subscription(s) encontrada(s)", len(subscriptions)))
	}

	type result struct {
		index  int
		config *ClusterConfig
		err    error
	}

	resultChan := make(chan result, len(aksClusters))
	var wg sync.WaitGroup

	// Semáforo externo conservador para WSL2: 5 clusters simultâneos (outer) × 10 subscriptions (inner) = máx 50 processos az
	semaphore := make(chan struct{}, 5)

	for i, cluster := range aksClusters {
		wg.Add(1)
		go func(idx int, clusterName string) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				resultChan <- result{index: idx, err: fmt.Errorf("timeout aguardando slot")}
				return
			}

			if ctx.Err() != nil {
				resultChan <- result{index: idx, err: ctx.Err()}
				return
			}

			config, err := k.AutoDiscoverClusterConfig(ctx, clusterName, subscriptions)
			resultChan <- result{index: idx, config: config, err: err}
		}(i, cluster.Name)
	}

	// Goroutine para fechar o canal quando todas as goroutines terminarem
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Coletar resultados
	results := make([]result, len(aksClusters))
	count := 0

	for res := range resultChan {
		results[res.index] = res
		count++

		if logFunc != nil {
			if res.err != nil {
				logFunc(fmt.Sprintf("[AKS] [%d/%d] ❌ %s: %v", count, len(aksClusters), aksClusters[res.index].Name, res.err))
			} else {
				subIDPreview := res.config.SubscriptionID
				if len(subIDPreview) > 8 {
					subIDPreview = subIDPreview[:8] + "..."
				}
				logFunc(fmt.Sprintf("[AKS] [%d/%d] ✅ %s — RG: %s, Sub: %s (ID: %s)",
					count, len(aksClusters),
					aksClusters[res.index].Name,
					res.config.ResourceGroup,
					res.config.Subscription,
					subIDPreview))
			}
		}
	}

	// Separar sucessos e erros
	var configs []ClusterConfig
	var errors []error

	for i, res := range results {
		if res.err != nil {
			errors = append(errors, fmt.Errorf("cluster %s: %w", aksClusters[i].Name, res.err))
		} else if res.config != nil {
			configs = append(configs, *res.config)
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 📊 Resumo: ✅ %d sucesso | ❌ %d erros", len(configs), len(errors)))
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

// SwitchContext muda o contexto ativo do Kubernetes para o cluster especificado
func (k *KubeConfigManager) SwitchContext(context string) error {
	// Verificar se o contexto existe
	if _, exists := k.config.Contexts[context]; !exists {
		return fmt.Errorf("context %s not found in kubeconfig", context)
	}

	// Executar kubectl config use-context
	cmd := exec.Command("kubectl", "config", "use-context", context)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to switch kubectl context to %s: %w, output: %s", context, err, string(output))
	}

	// Atualizar contexto atual na configuração em memória
	k.config.CurrentContext = context

	// Limpar cache de clientes para forçar recriação com novo contexto
	k.clientMutex.Lock()
	k.clients = make(map[string]kubernetes.Interface)
	k.clientMutex.Unlock()

	return nil
}

// SwitchAzureContext muda o contexto do Azure CLI para corresponder ao cluster Kubernetes
func (k *KubeConfigManager) SwitchAzureContext(contextName string) error {
	// Tentar primeiro na config salva (mais rápido que redescobrir)
	var clusterConfig *ClusterConfig
	configs := k.loadClustersFromConfig()
	for _, cfg := range configs {
		if cfg.Name == contextName {
			clusterConfig = &cfg
			break
		}
	}

	// Fallback: auto-descoberta completa
	if clusterConfig == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		subscriptions, err := k.loadAllAzureSubscriptions(ctx, nil)
		if err != nil {
			return fmt.Errorf("could not find Azure configuration for cluster %s: %w", contextName, err)
		}
		discovered, err := k.AutoDiscoverClusterConfig(ctx, contextName, subscriptions)
		if err != nil {
			return fmt.Errorf("could not find Azure configuration for cluster %s", contextName)
		}
		clusterConfig = discovered
	}
	_ = clusterConfig // usado abaixo

	// Mudar para a subscription correta
	cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to switch Azure subscription to %s: %w, output: %s",
			clusterConfig.Subscription, err, string(output))
	}

	return nil
}

// ClusterMetrics representa métricas do cluster
type ClusterMetrics struct {
	CPUUsagePercent       float64 `json:"cpuUsagePercent"`
	MemoryUsagePercent    float64 `json:"memoryUsagePercent"`
	CPUCapacityPercent    float64 `json:"cpuCapacityPercent"`    // % de Allocatable em relação ao Capacity
	MemoryCapacityPercent float64 `json:"memoryCapacityPercent"` // % de Allocatable em relação ao Capacity
	NodeCount             int     `json:"nodeCount"`
	PodCount              int     `json:"podCount"`
}

// GetKubernetesVersion obtém a versão do Kubernetes do cluster
func (k *KubeConfigManager) GetKubernetesVersion(clusterName string) (string, error) {
	client, err := k.getClient(clusterName)
	if err != nil {
		return "", fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err)
	}

	// Obter informações do servidor
	serverVersion, err := client.Discovery().ServerVersion()
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}

	return serverVersion.GitVersion, nil
}

// GetClusterMetrics obtém métricas básicas do cluster
// Tenta usar Metrics Server para métricas reais, fallback para requests
func (k *KubeConfigManager) GetClusterMetrics(clusterName string) (*ClusterMetrics, error) {
	client, err := k.getClient(clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Obter contagem de nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Obter contagem de pods
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Calcular totais de Capacity e Allocatable
	var totalCPUCapacity, totalCPUAllocatable, totalCPUUsage int64
	var totalMemoryCapacity, totalMemoryAllocatable, totalMemoryUsage int64

	for _, node := range nodes.Items {
		// Capacity (hardware total)
		if cpu := node.Status.Capacity.Cpu(); cpu != nil {
			totalCPUCapacity += cpu.MilliValue()
		}
		if memory := node.Status.Capacity.Memory(); memory != nil {
			totalMemoryCapacity += memory.Value()
		}

		// Allocatable (disponível para pods)
		if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
			totalCPUAllocatable += cpu.MilliValue()
		}
		if memory := node.Status.Allocatable.Memory(); memory != nil {
			totalMemoryAllocatable += memory.Value()
		}

		// Uso atual (pods alocados no node) - apenas para fallback
		fieldSelector := fmt.Sprintf("spec.nodeName=%s", node.Name)
		nodePods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fieldSelector,
		})
		if err != nil {
			continue
		}

		for _, pod := range nodePods.Items {
			if pod.Status.Phase != "Running" {
				continue
			}
			for _, container := range pod.Spec.Containers {
				if cpu := container.Resources.Requests.Cpu(); cpu != nil {
					totalCPUUsage += cpu.MilliValue()
				}
				if memory := container.Resources.Requests.Memory(); memory != nil {
					totalMemoryUsage += memory.Value()
				}
			}
		}
	}

	// Tentar obter métricas reais via Metrics Server
	realMetrics, hasRealMetrics := k.getRealNodeMetrics(client, ctx, nodes.Items, totalCPUAllocatable, totalMemoryAllocatable)

	// Usar métricas reais se disponíveis, senão usar requests como fallback
	var cpuPercent, memoryPercent float64
	if hasRealMetrics {
		cpuPercent = realMetrics.CPUUsagePercent
		memoryPercent = realMetrics.MemoryUsagePercent
	} else {
		// Fallback: calcular baseado em requests vs allocatable
		if totalCPUAllocatable > 0 {
			cpuPercent = float64(totalCPUUsage) / float64(totalCPUAllocatable) * 100
		}
		if totalMemoryAllocatable > 0 {
			memoryPercent = float64(totalMemoryUsage) / float64(totalMemoryAllocatable) * 100
		}
	}

	// Calcular % de Allocatable em relação ao Capacity (overhead do sistema)
	var cpuCapacityPercent, memoryCapacityPercent float64
	if totalCPUCapacity > 0 {
		cpuCapacityPercent = float64(totalCPUAllocatable) / float64(totalCPUCapacity) * 100
	}
	if totalMemoryCapacity > 0 {
		memoryCapacityPercent = float64(totalMemoryAllocatable) / float64(totalMemoryCapacity) * 100
	}

	return &ClusterMetrics{
		CPUUsagePercent:       cpuPercent,
		MemoryUsagePercent:    memoryPercent,
		CPUCapacityPercent:    cpuCapacityPercent,
		MemoryCapacityPercent: memoryCapacityPercent,
		NodeCount:             len(nodes.Items),
		PodCount:              len(pods.Items),
	}, nil
}

// getRealNodeMetrics tenta obter métricas reais via Metrics Server
func (k *KubeConfigManager) getRealNodeMetrics(client kubernetes.Interface, ctx context.Context, nodes []corev1.Node, totalCPUAllocatable, totalMemoryAllocatable int64) (*ClusterMetrics, bool) {
	// Tentar acessar Metrics Server API via Discovery client
	discoveryClient := client.Discovery()

	// Fazer request para /apis/metrics.k8s.io/v1beta1/nodes
	result := discoveryClient.RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/nodes").
		Do(ctx)

	rawData, err := result.Raw()
	if err != nil {
		return nil, false
	}

	// Parse da resposta JSON
	var nodeMetrics struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"items"`
	}

	if err := json.Unmarshal(rawData, &nodeMetrics); err != nil {
		return nil, false
	}

	// Calcular totais de uso real
	var totalCPUUsage, totalMemoryUsage int64
	for _, metric := range nodeMetrics.Items {
		// Parse CPU usage (formato: "123m" ou "1.5")
		if cpuQuantity, err := resource.ParseQuantity(metric.Usage.CPU); err == nil {
			totalCPUUsage += cpuQuantity.MilliValue()
		}

		// Parse Memory usage (formato: "1234567Ki")
		if memoryQuantity, err := resource.ParseQuantity(metric.Usage.Memory); err == nil {
			totalMemoryUsage += memoryQuantity.Value()
		}
	}

	// Calcular percentuais baseado no Allocatable
	var cpuPercent, memoryPercent float64
	if totalCPUAllocatable > 0 {
		cpuPercent = float64(totalCPUUsage) / float64(totalCPUAllocatable) * 100
	}
	if totalMemoryAllocatable > 0 {
		memoryPercent = float64(totalMemoryUsage) / float64(totalMemoryAllocatable) * 100
	}

	return &ClusterMetrics{
		CPUUsagePercent:    cpuPercent,
		MemoryUsagePercent: memoryPercent,
	}, true
}

// GetClusterConfigFromFile retorna a configuração de um cluster do arquivo clusters-config.json
func (k *KubeConfigManager) GetClusterConfigFromFile(clusterName string) (map[string]interface{}, error) {
	configs := k.loadClustersFromConfig()

	for _, cfg := range configs {
		if cfg.Name == clusterName {
			return map[string]interface{}{
				"clusterName":   cfg.Name,
				"resourceGroup": cfg.ResourceGroup,
				"subscription":  cfg.Subscription,
			}, nil
		}
	}

	return nil, fmt.Errorf("cluster configuration not found for: %s", clusterName)
}

// SwitchAzureSubscription muda para uma subscription específica do Azure
func (k *KubeConfigManager) SwitchAzureSubscription(subscription string) error {
	cmd := exec.Command("az", "account", "set", "--subscription", subscription)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to switch Azure subscription to %s: %w, output: %s",
			subscription, err, string(output))
	}
	return nil
}
