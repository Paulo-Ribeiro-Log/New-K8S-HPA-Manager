package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
	clientauthenticationv1beta1 "k8s.io/client-go/pkg/apis/clientauthentication/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"k8s-hpa-manager/internal/cloudprovider"
	awsprovider "k8s-hpa-manager/internal/cloudprovider/aws"
	azureprovider "k8s-hpa-manager/internal/cloudprovider/azure"
	gcpprovider "k8s-hpa-manager/internal/cloudprovider/gcp"
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

// reachabilityCacheTTL define por quanto tempo o resultado do probe TCP é reaproveitado.
// Evita dialar a cada requisição, mas garante detecção rápida de VPN/rede fora do ar.
const reachabilityCacheTTL = 15 * time.Second

// reachabilityProbeTimeout é o timeout do dial TCP usado para o probe de conectividade.
// Bem menor que restConfig.Timeout (30s) para que a falha apareça rápido ao usuário.
const reachabilityProbeTimeout = 3 * time.Second

// eksTokenTimeout limita a execução do subprocesso `aws eks get-token`. O exec credential
// plugin nativo do client-go (vendor/k8s.io/client-go/plugin/pkg/client/auth/exec/exec.go)
// roda exec.Command SEM nenhum timeout — se a sessão AWS SSO do perfil estiver expirada,
// o processo pode travar indefinidamente. Gerar o token nós mesmos com um timeout curto
// evita esse travamento e permite cair no fallback (ExecProvider nativo) rapidamente.
const eksTokenTimeout = 10 * time.Second

// eksTokenSafetyBuffer é subtraído da expiração real do token STS ao definir o TTL do
// cache em memória — evita usar um token perto de expirar numa requisição em andamento.
const eksTokenSafetyBuffer = 1 * time.Minute

// eksTokenFallbackTTL é usado quando a resposta de `aws eks get-token` não traz
// ExpirationTimestamp (não deveria acontecer, mas evita cachear indefinidamente).
const eksTokenFallbackTTL = 10 * time.Minute

type restConfigEntry struct {
	config *rest.Config
	exp    time.Time
}

type reachabilityEntry struct {
	reachable bool
	exp       time.Time
}

// KubeConfigManager gerencia a configuração do Kubernetes
type KubeConfigManager struct {
	configPath     string
	config         *api.Config
	clients        map[string]kubernetes.Interface
	clientMutex    sync.RWMutex // Protege acesso concorrente aos clients
	metricsClients map[string]*metricsclientset.Clientset
	metricsMutex   sync.RWMutex
	restConfigs    map[string]*restConfigEntry // Cache de *rest.Config por cluster
	restConfigsMu  sync.RWMutex
	reachability   map[string]*reachabilityEntry // Cache curto de probe TCP por cluster
	reachabilityMu sync.RWMutex
	lastUsed       map[string]time.Time // Último acesso por cluster (clients + metricsClients)
	lastUsedMutex  sync.Mutex
	historyTracker *history.HistoryTracker
}

// snapshotKubeconfig copia sourcePath para uma cópia privada do app
// (~/.k8s-hpa-manager/kubeconfig, sempre recriada) e retorna o caminho da cópia.
//
// Motivo: mesmo depois de SwitchContext parar de escrever `current-context` no arquivo
// compartilhado (ver histórico de bug de corrupção), GetRestConfig ainda LÊ o kubeconfig
// original do disco a cada cache-miss (a cada ~30-40min por cluster) via
// clientcmd.ClientConfigLoadingRules — concorrendo com escritas de outras ferramentas
// (kubectl, k9s, `az aks get-credentials`, `aws eks update-kubeconfig`, `gcloud container
// clusters get-credentials`) que também usam ~/.kube/config. Uma leitura no meio de uma
// escrita externa pode pegar YAML parcial/inválido e derrubar a resolução de todos os
// clusters, não só o que estava sendo escrito. Tirar uma cópia própria uma única vez no
// startup do processo e trabalhar só em cima dela elimina esse acoplamento por completo —
// o processo nunca mais toca o arquivo original depois de copiá-lo. Trade-off aceito:
// mudanças no kubeconfig original (novo contexto, credencial renovada) só são vistas após
// reiniciar o app — já era o comportamento de fato, já que k.config (lista de contexts) só
// é carregado uma vez em memória mesmo sem essa cópia.
func snapshotKubeconfig(sourcePath string) (string, error) {
	if sourcePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to determine kubeconfig path: %w", err)
		}
		sourcePath = filepath.Join(home, ".kube", "config")
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig at %s: %w", sourcePath, err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	appDir := filepath.Join(home, ".k8s-hpa-manager")
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create app config dir: %w", err)
	}

	dest := filepath.Join(appDir, "kubeconfig")

	// Nome do arquivo temporário único por chamada (não um `dest+".tmp"` fixo): mais de um
	// KubeConfigManager pode ser criado concorrentemente no mesmo processo (ex: cordon/drain
	// em internal/web/handlers/nodepools.go instancia um manager próprio por request). Um nome
	// fixo faria duas goroutines escreverem no mesmo arquivo tmp ao mesmo tempo — exatamente o
	// tipo de corrupção por concorrência que esse mecanismo existe pra evitar. Com nome único,
	// cada goroutine escreve seu próprio arquivo intacto e só a renomeação final (atômica a
	// nível de SO) disputa `dest` — a última vence, sem nunca deixar `dest` num estado parcial.
	tmpFile, err := os.CreateTemp(appDir, "kubeconfig-*.tmp")
	if err != nil {
		return "", fmt.Errorf("failed to create kubeconfig snapshot temp file: %w", err)
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("failed to write kubeconfig snapshot: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("failed to write kubeconfig snapshot: %w", err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("failed to set kubeconfig snapshot permissions: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("failed to finalize kubeconfig snapshot: %w", err)
	}

	return dest, nil
}

// NewKubeConfigManager cria um novo gerenciador de kubeconfig. A cada chamada, tira um
// snapshot privado do kubeconfig de origem (ver snapshotKubeconfig) e opera exclusivamente
// sobre essa cópia — nunca sobre o arquivo compartilhado.
func NewKubeConfigManager(configPath string) (*KubeConfigManager, error) {
	privatePath, err := snapshotKubeconfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot kubeconfig: %w", err)
	}

	config, err := clientcmd.LoadFromFile(privatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	km := &KubeConfigManager{
		configPath:     privatePath,
		config:         config,
		clients:        make(map[string]kubernetes.Interface),
		metricsClients: make(map[string]*metricsclientset.Clientset),
		restConfigs:    make(map[string]*restConfigEntry),
		reachability:   make(map[string]*reachabilityEntry),
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

	k.restConfigsMu.Lock()
	for _, cluster := range stale {
		delete(k.restConfigs, cluster)
	}
	k.restConfigsMu.Unlock()

	k.reachabilityMu.Lock()
	for _, cluster := range stale {
		delete(k.reachability, cluster)
	}
	k.reachabilityMu.Unlock()

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
// Detecta automaticamente o cloud provider (AKS/EKS/GKE) via URL do API server.
// Quando há clusters GKE, garante que gke-gcloud-auth-plugin está instalado.
func (k *KubeConfigManager) DiscoverClusters() []models.Cluster {
	// clusterName (kubeconfig) → context name
	clusterToContext := make(map[string]string)

	var hasGKE bool
	for contextName, ctx := range k.config.Contexts {
		clusterToContext[ctx.Cluster] = contextName
		if strings.HasPrefix(contextName, "gke_") {
			hasGKE = true
		}
	}

	// Garantir o plugin de autenticação GKE antes de qualquer operação de cliente
	if hasGKE {
		if err := gcpprovider.EnsureGKEAuthPlugin(nil); err != nil {
			fmt.Printf("⚠️  [GKE] %v\n", err)
		}
		// Carregar ADC salvo pela app (gcp-adc.json) para que gke-gcloud-auth-plugin
		// encontre as credenciais sem depender de gcloud auth login explícito.
		gcpprovider.LoadSavedGCPADC()
		// Pré-aquecer o cache de token GKE em background: quando o usuário selecionar
		// um cluster GKE, o token já estará pronto (evita 15s de espera na primeira requisição).
		go func() { gcpprovider.GetFreshGKEToken(context.Background()) }()
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
		contextName := clusterToContext[clusterName]
		cloudProvider := DetectCloudProvider(serverURL, contextName)
		region := ""
		if cloudProvider == CloudProviderEKS {
			region = ExtractRegionFromEKSURL(serverURL)
		} else if cloudProvider == CloudProviderAKS {
			region = extractAKSRegion(serverURL)
		} else if cloudProvider == CloudProviderGKE {
			_, region, _ = splitGKEContext(contextName)
		}

		displayName := clusterName
		awsProfile := ""
		if cloudProvider == CloudProviderEKS {
			// ARN arn:aws:eks:REGION:ACCOUNT:cluster/NAME → NAME
			if idx := strings.LastIndex(clusterName, "/"); idx >= 0 {
				displayName = clusterName[idx+1:]
			}
			// Expor o perfil AWS real (do kubeconfig) para que o frontend use sem inferência
			awsProfile = k.resolveAWSProfile(contextName)
		} else if cloudProvider == CloudProviderGKE {
			_, _, shortName := splitGKEContext(contextName)
			if shortName != "" {
				displayName = shortName
			}
		}

		clusters = append(clusters, models.Cluster{
			Name:          displayName,
			Context:       contextName,
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

// GetClusterConfig retorna a configuração AKS de um cluster pelo nome (sem sufixo -admin).
func (k *KubeConfigManager) GetClusterConfig(clusterName string) *ClusterConfig {
	bare := strings.TrimSuffix(clusterName, "-admin")
	for _, c := range k.loadClustersFromConfig() {
		n := strings.TrimSuffix(c.Name, "-admin")
		if n == bare || c.Name == clusterName {
			return &c
		}
	}
	return nil
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
// (4) busca reversa por ARN EKS terminando em "/<name>" (suporta nome curto do cluster),
// (5) busca reversa por context GKE terminando em "_<name>" (suporta nome curto do cluster).
// Isso permite que o sistema funcione com AKS (com/sem -admin), EKS (ARN completo ou nome
// curto) e GKE (context "gke_<project>_<region>_<cluster>" ou nome curto), sem exigir
// normalização no frontend ou nos handlers.
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
	// Busca reversa para GKE: nome curto "gke-higgs-hlg" → contexto "gke_<project>_<region>_gke-higgs-hlg".
	// Necessário porque models.NodePool.ClusterName (GCPNodeGroupProvider) usa o nome curto do
	// cluster, não o context completo — ex: a aba "Nodes" do NodePoolEditor chama
	// GET /nodes/:cluster/:nodepool com esse nome curto.
	if name != "" && !strings.HasPrefix(name, "gke_") {
		gkeSuffix := "_" + name
		for contextName := range k.config.Contexts {
			if strings.HasPrefix(contextName, "gke_") && strings.HasSuffix(contextName, gkeSuffix) {
				return contextName
			}
		}
	}
	return name // retorna original para que o erro eventual seja descritivo
}

// ResolveContext expõe resolveContext publicamente — usado por operações que chamam o binário
// `kubectl` diretamente via subprocess (ex: ExecuteKubectlDescribe) em vez do client-go, e por
// isso não passam pelo GetClient/getClient que já resolvem isso internamente. Sem isso, `kubectl
// --context <nome-curto>` falha ou (pior, silenciosamente) aponta pro cluster errado em GKE/EKS,
// onde o nome exibido na UI não é o nome real do context no kubeconfig.
func (k *KubeConfigManager) ResolveContext(name string) string {
	return k.resolveContext(name)
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

// checkReachability faz um probe TCP rápido (cacheado por reachabilityCacheTTL) para
// detectar cluster inacessível (VPN/rede fora do ar) sem pagar o timeout completo do
// client K8s (restConfig.Timeout = 30s) em toda chamada. Sem isso, quando a VPN cai,
// cada requisição de HPAs/deployments/pods/etc trava 30s antes de falhar.
func (k *KubeConfigManager) checkReachability(clusterName string) error {
	// Resolver o contexto real primeiro — sem isso, nomes curtos de cluster (ex: GKE
	// "gke-higgs-hlg" vindo de models.NodePool.ClusterName, que não é o context completo
	// "gke_<project>_<region>_<cluster>") não batem com nenhum cluster no kubeconfig e o probe
	// de rede sempre falha, mesmo com o cluster acessível.
	resolved := k.resolveContext(clusterName)

	k.reachabilityMu.RLock()
	if entry, ok := k.reachability[resolved]; ok && time.Now().Before(entry.exp) {
		k.reachabilityMu.RUnlock()
		if !entry.reachable {
			return fmt.Errorf("cluster %s inacessível — verifique a VPN/conectividade de rede", clusterName)
		}
		return nil
	}
	k.reachabilityMu.RUnlock()

	reachable := k.TestClusterTCPConnection(resolved, reachabilityProbeTimeout)

	k.reachabilityMu.Lock()
	k.reachability[resolved] = &reachabilityEntry{reachable: reachable, exp: time.Now().Add(reachabilityCacheTTL)}
	k.reachabilityMu.Unlock()

	if !reachable {
		return fmt.Errorf("cluster %s inacessível — verifique a VPN/conectividade de rede", clusterName)
	}
	return nil
}

// GetRestConfig retorna a configuração REST para o cluster especificado.
// O resultado é cacheado (40min para GKE, 30min para outros) para evitar
// token fetches repetidos em chamadas concorrentes.
func (k *KubeConfigManager) GetRestConfig(clusterName string) (*rest.Config, error) {
	if err := k.checkReachability(clusterName); err != nil {
		return nil, err
	}

	// Fast path: cache hit
	k.restConfigsMu.RLock()
	if entry, ok := k.restConfigs[clusterName]; ok && time.Now().Before(entry.exp) {
		cfg := rest.CopyConfig(entry.config)
		k.restConfigsMu.RUnlock()
		return cfg, nil
	}
	k.restConfigsMu.RUnlock()

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
	// Passar resolved e clusterName — GKE usa prefixo gke_ no context name.
	cloudProvider := DetectCloudProvider(serverURL, resolved, clusterName)

	if cloudProvider == CloudProviderEKS && len(restConfig.CertData) == 0 && restConfig.KeyFile == "" {
		if restConfig.ExecProvider != nil {
			// Kubeconfig já tem exec provider configurado (ex: `aws eks update-kubeconfig --profile X`).
			// Respeitar como está — mesmo comportamento do K9s/kubectl.
		} else if exec := k.buildEKSExecProvider(resolved, serverURL, clusterName); exec != nil {
			// Preferir gerar e cachear o token nós mesmos (com timeout curto) em vez de
			// deixar por conta do ExecProvider nativo do client-go, que roda `aws eks get-token`
			// sem nenhum timeout e sem cache de app — uma sessão AWS SSO expirada travaria a
			// troca de cluster indefinidamente. Ver getFreshEKSToken.
			if token, ok := getFreshEKSToken(exec.Args); ok {
				restConfig.ExecProvider = nil
				restConfig.BearerToken = token // usado por KubectlAuthArgs (kubeconfig temporário de vida curta)
				// O client Go do clientset fica cacheado até clientTTL (30min) de IDLE — pode
				// viver bem mais que a validade real do token STS (~15min) se usado continuamente.
				// WrapTransport reescreve o header Authorization a cada requisição com o token
				// mais recente do cache (renovando sozinho quando expira), em vez de depender do
				// BearerToken estático travado no valor de quando o client foi criado.
				execArgs := exec.Args
				restConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
					return &eksTokenRoundTripper{args: execArgs, base: rt}
				}
			} else {
				// Fallback: comportamento anterior (ExecProvider nativo, sem cache/timeout
				// próprios) — preserva a UX existente (incl. EnrichEKSError) se nossa
				// geração direta falhar por qualquer motivo.
				restConfig.ExecProvider = exec
				restConfig.BearerToken = "" // Limpar token estático expirado
			}
		}
	}

	// Para clusters GKE, injetar BearerToken via ADC salvo ou `gcloud auth print-access-token`.
	// Isso evita dependência do gke-gcloud-auth-plugin quando o usuário já está autenticado.
	if cloudProvider == CloudProviderGKE {
		if token := gcpprovider.GetFreshGKEToken(context.Background()); token != "" {
			restConfig.ExecProvider = nil // ADC/gcloud têm prioridade sobre exec plugin
			restConfig.BearerToken = token
		}
	}

	// Cachear o rest config: 40min para GKE (token expira em 45min — buffer de 5min),
	// 30min para outros providers (igual ao client TTL).
	ttl := 30 * time.Minute
	if cloudProvider == CloudProviderGKE {
		ttl = 40 * time.Minute
	}
	k.restConfigsMu.Lock()
	k.restConfigs[clusterName] = &restConfigEntry{config: rest.CopyConfig(restConfig), exp: time.Now().Add(ttl)}
	k.restConfigsMu.Unlock()

	return restConfig, nil
}

// KubectlAuthArgs retorna os argumentos que subprocessos `kubectl` (usados para CRDs sem
// dynamic client — Gateway API, Resource Explorer, Describe) devem receber para se autenticar.
//
// Para clusters GKE, `kubectl --context <ctx>` lê o kubeconfig do sistema, cujo ExecProvider
// normalmente é o `gke-gcloud-auth-plugin`, que por sua vez invoca a sessão `gcloud auth login`
// local — uma credencial independente do ADC próprio da aplicação (gcp-adc.json). Se a sessão
// gcloud local expirar ("Reauthentication failed"), todo `kubectl` para GKE falha mesmo que o
// ADC da app esteja válido.
//
// Para clusters EKS, o mesmo problema existe em relação ao AWS CLI/perfil configurado no
// kubeconfig do sistema — sem um exec provider funcional lá (`aws eks update-kubeconfig`
// nunca rodado, ou token estático de 15min já expirado), o `kubectl --context <arn>` falha.
//
// Para evitar essa dependência, para GKE e EKS geramos um kubeconfig temporário com o
// Host/BearerToken/ExecProvider já resolvidos por GetRestConfig() (mesmo caminho usado pelo
// client-go em memória para essas duas clouds — ver os ramos CloudProviderGKE/CloudProviderEKS
// ali) e apontamos o kubectl para ele via `--kubeconfig`. Para AKS (e providers desconhecidos),
// GetRestConfig não injeta nada — o comportamento permanece `--context <cluster>` como sempre foi.
//
// O caller DEVE chamar cleanup() (mesmo em caso de erro) para remover o arquivo temporário.
func (k *KubeConfigManager) KubectlAuthArgs(cluster string) (args []string, cleanup func(), err error) {
	noop := func() {}

	resolved := k.resolveContext(cluster)
	serverURL := k.getServerURL(resolved)
	if serverURL == "" {
		serverURL = k.getServerURL(cluster)
	}
	cloudProvider := DetectCloudProvider(serverURL, resolved, cluster)

	if cloudProvider != CloudProviderGKE && cloudProvider != CloudProviderEKS {
		return []string{"--context", cluster}, noop, nil
	}

	restConfig, rcErr := k.GetRestConfig(cluster)
	if rcErr != nil || (restConfig.BearerToken == "" && restConfig.ExecProvider == nil) {
		// Sem token/exec provider resolvido — cai para o comportamento antigo (kubectl usa o
		// kubeconfig do sistema como está, incluindo o exec-plugin se houver).
		return []string{"--context", cluster}, noop, nil
	}

	if cloudProvider == CloudProviderEKS && restConfig.BearerToken != "" {
		// restConfig pode vir do cache de até 30min (k.restConfigs), mas o Bearer Token EKS
		// (STS via `aws eks get-token`) só é válido por ~15min. O clientset típico não sofre
		// com isso porque restConfig.WrapTransport (eksTokenRoundTripper) renova o header
		// Authorization a cada requisição HTTP — mas aqui o token é gravado ESTÁTICO num
		// kubeconfig temporário consumido por um subprocesso `kubectl`, que não tem esse
		// mecanismo de renovação. Sem isso, describe (e as demais chamadas via kubectl
		// subprocess) funcionava só nos primeiros ~15min de cada janela de cache de 30min e
		// falhava com 401/403 dali em diante. getFreshEKSToken tem cache próprio com TTL
		// derivado da expiração real do STS, então isso não gera subprocessos extras na
		// maioria das chamadas — só busca de novo quando o token realmente expirou.
		if exec := k.buildEKSExecProvider(resolved, serverURL, cluster); exec != nil {
			if token, ok := getFreshEKSToken(exec.Args); ok {
				restConfig.BearerToken = token
			}
		}
	}

	tmpKubeconfig := api.NewConfig()

	clusterEntry := api.NewCluster()
	clusterEntry.Server = restConfig.Host
	clusterEntry.CertificateAuthorityData = restConfig.CAData
	clusterEntry.InsecureSkipTLSVerify = restConfig.Insecure || len(restConfig.CAData) == 0
	tmpKubeconfig.Clusters["target"] = clusterEntry

	authInfo := api.NewAuthInfo()
	if restConfig.BearerToken != "" {
		authInfo.Token = restConfig.BearerToken
	} else {
		authInfo.Exec = restConfig.ExecProvider
	}
	tmpKubeconfig.AuthInfos["target-user"] = authInfo

	ctxEntry := api.NewContext()
	ctxEntry.Cluster = "target"
	ctxEntry.AuthInfo = "target-user"
	tmpKubeconfig.Contexts["target-context"] = ctxEntry
	tmpKubeconfig.CurrentContext = "target-context"

	tmpFile, err := os.CreateTemp("", "kubeconfig-"+cloudProvider+"-*.yaml")
	if err != nil {
		return nil, noop, fmt.Errorf("failed to create temp kubeconfig: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if err := clientcmd.WriteToFile(*tmpKubeconfig, tmpPath); err != nil {
		os.Remove(tmpPath)
		return nil, noop, fmt.Errorf("failed to write temp kubeconfig: %w", err)
	}

	return []string{"--kubeconfig", tmpPath}, func() { os.Remove(tmpPath) }, nil
}

// eksTokenCacheEntry armazena um bearer token EKS já obtido via `aws eks get-token`.
type eksTokenCacheEntry struct {
	token string
	exp   time.Time
}

// Cache de token EKS por conjunto de args (cluster+region+profile) — evita spawnar o
// subprocesso `aws eks get-token` a cada expiração do cache de rest.Config/clientset (30min)
// e, com o singleflight, evita N subprocessos concorrentes quando várias requisições chegam
// juntas logo após a troca de cluster no frontend (mesmo padrão de GetFreshGKEToken).
var (
	eksTokenCache   = make(map[string]*eksTokenCacheEntry)
	eksTokenCacheMu sync.Mutex
	eksTokenSF      singleflight.Group
)

// getFreshEKSToken executa (ou reaproveita do cache) `aws eks get-token` com os args
// fornecidos — os mesmos que seriam passados ao ExecProvider — decodificando a resposta
// ExecCredential (mesmo formato que o client-go usa nativamente).
//
// Diferente do exec credential plugin nativo do client-go (sem timeout, sem cache
// próprio de app — só o cache interno por instância de transporte), esta função:
//  1. Cacheia o token em memória com TTL derivado da expiração real do token STS.
//  2. Limita a execução do subprocesso a eksTokenTimeout — uma sessão AWS SSO expirada
//     não trava a troca de cluster indefinidamente.
//
// Retorna (token, true) em sucesso. Em qualquer falha (aws ausente, timeout, sessão SSO
// expirada, resposta inválida), retorna (_, false) e o chamador deve cair no ExecProvider
// nativo — preserva o comportamento anterior (e a mensagem de erro via EnrichEKSError).
func getFreshEKSToken(args []string) (string, bool) {
	key := strings.Join(args, "|")

	eksTokenCacheMu.Lock()
	if entry, ok := eksTokenCache[key]; ok && time.Now().Before(entry.exp) {
		token := entry.token
		eksTokenCacheMu.Unlock()
		return token, true
	}
	eksTokenCacheMu.Unlock()

	v, _, _ := eksTokenSF.Do(key, func() (interface{}, error) {
		eksTokenCacheMu.Lock()
		if entry, ok := eksTokenCache[key]; ok && time.Now().Before(entry.exp) {
			token := entry.token
			eksTokenCacheMu.Unlock()
			return token, nil
		}
		eksTokenCacheMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), eksTokenTimeout)
		defer cancel()

		out, err := exec.CommandContext(ctx, "aws", args...).Output()
		if err != nil {
			log.Warn().Err(err).Strs("args", args).Msg("[getFreshEKSToken] falha ao executar aws eks get-token — caindo no ExecProvider nativo")
			return "", nil
		}

		var cred clientauthenticationv1beta1.ExecCredential
		if err := json.Unmarshal(out, &cred); err != nil || cred.Status == nil || cred.Status.Token == "" {
			log.Warn().Err(err).Msg("[getFreshEKSToken] resposta de aws eks get-token inválida — caindo no ExecProvider nativo")
			return "", nil
		}

		exp := time.Now().Add(eksTokenFallbackTTL)
		if cred.Status.ExpirationTimestamp != nil {
			exp = cred.Status.ExpirationTimestamp.Time.Add(-eksTokenSafetyBuffer)
		}

		eksTokenCacheMu.Lock()
		eksTokenCache[key] = &eksTokenCacheEntry{token: cred.Status.Token, exp: exp}
		eksTokenCacheMu.Unlock()

		return cred.Status.Token, nil
	})

	token, _ := v.(string)
	return token, token != ""
}

// eksTokenRoundTripper reescreve o header Authorization em cada requisição com o token EKS
// mais recente disponível em cache (via getFreshEKSToken), garantindo renovação automática
// para clients de longa duração — o clientset K8s fica cacheado até clientTTL (30min) de
// idle, período que pode facilmente ultrapassar a validade real de um token STS (~15min).
// Isso substitui a dependência de um restConfig.BearerToken estático, que ficaria travado
// no valor obtido no momento da criação do client.
type eksTokenRoundTripper struct {
	args []string
	base http.RoundTripper
}

func (rt *eksTokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := getFreshEKSToken(rt.args); ok {
		req = utilnet.CloneRequest(req)
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return rt.base.RoundTrip(req)
}

func (rt *eksTokenRoundTripper) WrappedRoundTripper() http.RoundTripper { return rt.base }

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
	// Checar reachability mesmo quando o client já está cacheado — sem isso, uma VPN caída
	// não é detectada aqui e cada chamada K8s feita com o client cacheado trava até o
	// restConfig.Timeout (30s) antes de falhar.
	if err := k.checkReachability(clusterName); err != nil {
		return nil, err
	}

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
// Detecta o cloud provider via context name e URL do API server no kubeconfig.
// AKS → AzureNodeGroupProvider; EKS → AWSNodeGroupProvider; GKE → GCPNodeGroupProvider.
func (k *KubeConfigManager) GetNodeGroupProvider(clusterName string) cloudprovider.NodeGroupProvider {
	normalizedName := strings.TrimSuffix(clusterName, "-admin")

	// Detectar cloud provider pela URL + context name (GKE usa prefixo gke_)
	serverURL := k.getServerURL(clusterName)
	cloudProvider := DetectCloudProvider(serverURL, clusterName)

	configEntries := k.loadClustersFromConfig()

	switch cloudProvider {
	case CloudProviderAKS:
		for _, c := range configEntries {
			if strings.TrimSuffix(c.Name, "-admin") == normalizedName {
				return azureprovider.NewAzureNodeGroupProvider(c.Name, c.ResourceGroup, c.Subscription)
			}
		}
		return azureprovider.NewAzureNodeGroupProvider(normalizedName, "", "")

	case CloudProviderEKS:
		region := ExtractRegionFromEKSURL(serverURL)
		profile := k.getAWSProfileFromKubeconfig(clusterName)
		if eksConfig := k.GetEKSClusterConfig(clusterName); eksConfig != nil {
			if eksConfig.AwsRegion != "" {
				region = eksConfig.AwsRegion
			}
			if eksConfig.AwsProfile != "" {
				profile = eksConfig.AwsProfile
			}
		}
		return awsprovider.NewAWSNodeGroupProvider(normalizedName, region, profile)

	case CloudProviderGKE:
		// gke-clusters-config.json tem prioridade; fallback: parsear context name
		var project, region, shortName string
		if gkeConfig := k.GetGKEClusterConfig(clusterName); gkeConfig != nil {
			project = gkeConfig.ProjectID
			region = gkeConfig.Region
			shortName = gkeConfig.Name
		} else {
			project, region, shortName = splitGKEContext(clusterName)
			if shortName == "" {
				shortName = normalizedName
			}
		}
		return gcpprovider.NewGCPNodeGroupProvider(shortName, project, region)

	default:
		// URL não conclusiva — fallback para AKS se estiver no clusters-config.json
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

// loadAllAzureSubscriptions carrega IDs e nomes das subscriptions disponíveis via Azure CLI.
// Retorna (ids []string, names map[id]name, error).
// Chamada uma única vez em AutoDiscoverAllClusters.
// Se o token estiver expirado, tenta renovar automaticamente via az login.
func (k *KubeConfigManager) loadAllAzureSubscriptions(ctx context.Context, logFunc func(string)) ([]string, map[string]string, error) {
	// Validar token antes de qualquer operação
	tokCtx, tokCancel := context.WithTimeout(ctx, 15*time.Second)
	defer tokCancel()
	if err := exec.CommandContext(tokCtx, "az", "account", "get-access-token", "--only-show-errors").Run(); err != nil {
		if logFunc != nil {
			logFunc("[AKS] 🔑 Token Azure expirado — abrindo autenticador...")
		}
		loginCmd := exec.CommandContext(context.Background(), "az", "login")
		loginCmd.Stdout = io.Discard
		loginCmd.Stderr = os.Stderr
		if loginErr := loginCmd.Run(); loginErr != nil {
			return nil, nil, fmt.Errorf("az login falhou: %w — execute 'az login' manualmente e tente novamente", loginErr)
		}
	}

	listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
	defer listCancel()
	out, err := exec.CommandContext(listCtx, "az", "account", "list",
		"--query", "[].{id:id,name:name}", "-o", "json", "--only-show-errors").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list subscriptions (erro 401 indica token expirado - execute 'az login'): %w\nOutput: %s", err, string(out))
	}

	var entries []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
		return nil, nil, fmt.Errorf("parse az account list: %w", jsonErr)
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no valid subscriptions found")
	}

	ids := make([]string, 0, len(entries))
	names := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			ids = append(ids, e.ID)
			names[e.ID] = e.Name
		}
	}
	return ids, names, nil
}

// aksListEntry representa um cluster retornado por "az aks list".
type aksListEntry struct {
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
	ID            string `json:"id"`
}

// buildAKSClusterIndex chama "az aks list --subscription X" uma vez por subscription
// (paralelo, semáforo 4) e devolve um índice pelo nome do cluster (lowercase).
//
// Custo: M chamadas az (M = nº de subscriptions) em vez de N×M (N clusters × M subs).
// Cada "az aks list" retorna todos os clusters da subscription de uma vez.
func (k *KubeConfigManager) buildAKSClusterIndex(
	ctx context.Context,
	subscriptions []string,
	subNames map[string]string,
	logFunc func(string),
) map[string]ClusterConfig {
	type subResult struct {
		entries []aksListEntry
		subID   string
	}

	sem := make(chan struct{}, 4)
	ch := make(chan subResult, len(subscriptions))
	var wg sync.WaitGroup

	for _, subID := range subscriptions {
		wg.Add(1)
		go func(sID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			out, err := exec.CommandContext(cmdCtx, "az", "aks", "list",
				"--subscription", sID,
				"--query", "[].{name:name,resourceGroup:resourceGroup,id:id}",
				"-o", "json",
				"--only-show-errors",
			).Output()
			if err != nil {
				if logFunc != nil {
					preview := sID
					if len(preview) > 8 {
						preview = preview[:8] + "..."
					}
					logFunc(fmt.Sprintf("[AKS] ⚠️  az aks list sub %s: %v", preview, err))
				}
				ch <- subResult{subID: sID}
				return
			}

			var entries []aksListEntry
			if jsonErr := json.Unmarshal(out, &entries); jsonErr != nil {
				ch <- subResult{subID: sID}
				return
			}
			ch <- subResult{entries: entries, subID: sID}
		}(subID)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	index := make(map[string]ClusterConfig)
	for res := range ch {
		for _, e := range res.entries {
			subID := extractSubscriptionIDFromARMID(e.ID)
			if subID == "" {
				subID = res.subID
			}
			subName := subNames[subID]
			if subName == "" {
				subName = subID
			}
			index[strings.ToLower(e.Name)] = ClusterConfig{
				Name:           e.Name,
				ResourceGroup:  e.ResourceGroup,
				Subscription:   subName,
				SubscriptionID: subID,
			}
		}
	}
	return index
}

// extractSubscriptionIDFromARMID extrai o subscription ID de um ARM ID.
// Formato: /subscriptions/SUBID/resourceGroups/...
func extractSubscriptionIDFromARMID(armID string) string {
	parts := strings.Split(armID, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "subscriptions") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// discoverSubscriptionViaAzureCLI descobre a subscription de um único cluster AKS
// verificando cada subscription via "az aks show" (path de fallback — usado apenas
// quando o cluster não foi encontrado via buildAKSClusterIndex).
func (k *KubeConfigManager) discoverSubscriptionViaAzureCLI(ctx context.Context, clusterName, resourceGroup string, validSubscriptions []string) (string, string, error) {
	type result struct {
		subscriptionID string
		resourceID     string
		err            error
	}

	resultChan := make(chan result, len(validSubscriptions))
	semaphore := make(chan struct{}, 3) // cap. 3 — az é Python, cold-start caro
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

			cmdCtx, cmdCancel := context.WithTimeout(subCtx, 20*time.Second)
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
//
// Estratégia O(M) em vez de O(N×M):
//   - Chama "az aks list --subscription X" uma vez por subscription (M chamadas, paralelo 4)
//   - Constrói índice clusterName → ClusterConfig
//   - Faz lookup de cada cluster do kubeconfig no índice (zero chamadas az extras)
//
// Antes: 5 clusters × 10 subscriptions = 50 processos az simultâneos (cada az = Python cold-start ~100MB)
// Agora: 4 subscriptions simultâneas → ≤4 processos az ao mesmo tempo
func (k *KubeConfigManager) AutoDiscoverAllClusters(logFunc func(string)) ([]ClusterConfig, []error) {
	clusters := k.DiscoverClusters()

	var aksClusters []models.Cluster
	for _, c := range clusters {
		if c.CloudProvider != CloudProviderEKS {
			aksClusters = append(aksClusters, c)
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 🔍 Iniciando auto-descoberta para %d clusters AKS...", len(aksClusters)))
	}

	if len(aksClusters) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if logFunc != nil {
		logFunc("[AKS] 📋 Carregando subscriptions Azure...")
	}
	subscriptions, subNames, err := k.loadAllAzureSubscriptions(ctx, logFunc)
	if err != nil {
		return nil, []error{err}
	}
	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 📋 %d subscription(s) — construindo índice de clusters...", len(subscriptions)))
	}

	// Uma chamada "az aks list" por subscription → índice completo
	index := k.buildAKSClusterIndex(ctx, subscriptions, subNames, logFunc)

	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 📋 Índice construído: %d cluster(s) encontrado(s) nas subscriptions", len(index)))
	}

	var configs []ClusterConfig
	var errors []error

	for i, cluster := range aksClusters {
		// Normaliza: remove sufixo -admin e lowercase para lookup
		lookupName := strings.ToLower(strings.TrimSuffix(cluster.Name, "-admin"))
		cfg, found := index[lookupName]
		if !found {
			errors = append(errors, fmt.Errorf("cluster %s: não encontrado em nenhuma subscription", cluster.Name))
			if logFunc != nil {
				logFunc(fmt.Sprintf("[AKS] [%d/%d] ❌ %s: não encontrado no índice", i+1, len(aksClusters), cluster.Name))
			}
			continue
		}
		// Preserva o nome original do kubeconfig (pode ter -admin)
		cfg.Name = cluster.Name
		configs = append(configs, cfg)
		if logFunc != nil {
			subIDPreview := cfg.SubscriptionID
			if len(subIDPreview) > 8 {
				subIDPreview = subIDPreview[:8] + "..."
			}
			logFunc(fmt.Sprintf("[AKS] [%d/%d] ✅ %s — RG: %s, Sub: %s (ID: %s)",
				i+1, len(aksClusters), cluster.Name, cfg.ResourceGroup, cfg.Subscription, subIDPreview))
		}
	}

	if logFunc != nil {
		logFunc(fmt.Sprintf("[AKS] 📊 Resumo: ✅ %d sucesso | ❌ %d não encontrados", len(configs), len(errors)))
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

// SwitchContext muda o contexto ativo do Kubernetes para o cluster especificado.
//
// Atualiza apenas o estado em memória (k.config.CurrentContext) — NUNCA chamar
// `kubectl config use-context` aqui. Todo o resto do app (GetRestConfig, chamadas
// exec.Command("kubectl", ...), Helm via --kube-context) já resolve o context pelo
// nome explicitamente, sem depender do current-context em disco. Reescrever
// ~/.kube/config a cada troca de cluster na Web UI só arrisca corromper o arquivo
// compartilhado — dois cliques de troca concorrentes (duas abas, dois usuários) ou
// uma edição manual do kubectl feita ao mesmo tempo competem pelo mesmo arquivo sem
// nenhum lock, podendo gerar YAML inválido. Ver histórico de investigação desse bug.
func (k *KubeConfigManager) SwitchContext(context string) error {
	// Verificar se o contexto existe
	if _, exists := k.config.Contexts[context]; !exists {
		return fmt.Errorf("context %s not found in kubeconfig", context)
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
	// Contextos GKE ("gke_...") e EKS ("arn:aws:eks:...") nunca têm configuração Azure.
	// Sem esse early-exit, o fallback de auto-descoberta abaixo lista todas as subscriptions
	// e escaneia "az aks list" em cada uma (via buildAKSClusterIndex) só para falhar no final —
	// isso é o que fazia a troca para um cluster GKE levar ~1min no ClusterHandler.SwitchContext.
	if strings.HasPrefix(contextName, "gke_") || strings.HasPrefix(contextName, "arn:aws:eks:") {
		return nil
	}

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
		subscriptions, _, err := k.loadAllAzureSubscriptions(ctx, nil)
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
