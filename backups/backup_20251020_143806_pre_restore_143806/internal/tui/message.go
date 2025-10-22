package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/session"
)

// Mensagem para forçar redesenho da tela
type clearScreenMsg struct{}

// Mensagem para atualizar progress bar de rollouts
type progressUpdateMsg struct{}

// Mensagem para limpar rollouts concluídos
type cleanupRolloutsMsg struct{}

// Mensagem para limpar status e mensagens de erro/sucesso
type clearStatusMsg struct{}

// Mensagem para auto-refresh do painel Status (a cada 2 segundos)
type statusRefreshMsg struct{}

// Mensagem para adicionar logs ao StatusPanel
type statusLogMsg struct {
	level   string // "info", "error", "success", "warn", "debug"
	source  string
	message string
}

// Mensagens para inicialização
type initManagersMsg struct {
	kubeManager    *config.KubeConfigManager
	sessionManager *session.Manager
	err            error
}

// Mensagens para descoberta de clusters
type clustersDiscoveredMsg struct {
	clusters []models.Cluster
	err      error
}

type clusterConnectionTestMsg struct {
	cluster string
	status  models.ConnectionStatus
	err     error
}

// Mensagens para namespaces
type namespacesLoadedMsg struct {
	namespaces []models.Namespace
	err        error
}

// Mensagens para HPAs
type hpasLoadedMsg struct {
	hpas []models.HPA
	err  error
}

// Mensagem para aplicação de mudanças
type hpaChangesAppliedMsg struct {
	count       int
	appliedHPAs []models.HPA // HPAs que foram aplicados com sucesso
	err         error
}

// Mensagem para contagem de HPAs
type hpaCountUpdatedMsg struct {
	namespace string
	count     int
	err       error
}

// Mensagem para salvamento de sessão
type sessionSavedMsg struct {
	sessionName string
	err         error
}

// Mensagem para deleção de sessão
type sessionDeletedMsg struct {
	sessionName string
	err         error
}

// Mensagem para renome de sessão
type sessionRenamedMsg struct {
	oldName string
	newName string
	err     error
}

// Mensagem para carregamento de sessões
type sessionsLoadedMsg struct {
	sessions []models.Session
	err      error
}

// Mensagem para carregamento de pastas de sessão
type sessionFoldersLoadedMsg struct {
	folders []string
	err     error
}

// Mensagem para carregamento do estado da sessão
type sessionStateLoadedMsg struct {
	clusterName string
	namespaces  []models.Namespace
	hpas        []models.HPA
	nodePools   []models.NodePool
	sessionName string
	err         error
}

// Mensagem para notificar que HPAs da sessão foram enriquecidos
type sessionHPAsEnrichedMsg struct {
	enrichedCount int
}


// discoverClusters descobre clusters disponíveis
func (a *App) discoverClusters() tea.Cmd {
	return func() tea.Msg {
		if a.kubeManager == nil {
			return clustersDiscoveredMsg{err: fmt.Errorf("kube manager not initialized")}
		}

		clusters := a.kubeManager.DiscoverClusters()
		// Simular alguns clusters para demonstração
		if len(clusters) == 0 {
			clusters = []models.Cluster{
				{Name: "aks-teste-prd", Context: "aks-teste-prd", Status: models.StatusUnknown},
				{Name: "aks-dev-cluster", Context: "aks-dev-cluster", Status: models.StatusUnknown},
			}
		}

		return clustersDiscoveredMsg{clusters: clusters, err: nil}
	}
}

// testClusterConnections testa conexões com clusters
func (a *App) testClusterConnections() tea.Cmd {
	if len(a.model.Clusters) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for _, cluster := range a.model.Clusters {
		cmds = append(cmds, a.testSingleClusterConnection(cluster.Name))
	}

	return tea.Batch(cmds...)
}

// testSingleClusterConnection testa conexão com um cluster específico
func (a *App) testSingleClusterConnection(clusterName string) tea.Cmd {
	return func() tea.Msg {
		if a.kubeManager == nil {
			return clusterConnectionTestMsg{
				cluster: clusterName,
				status:  models.StatusError,
				err:     fmt.Errorf("kube manager not initialized"),
			}
		}

		// Testar conexão real com o cluster
		status := a.kubeManager.TestClusterConnection(a.ctx, clusterName)
		return clusterConnectionTestMsg{
			cluster: clusterName,
			status:  status,
			err:     nil,
		}
	}
}

// Mensagens para Azure Authentication
type azureAuthStartMsg struct{}
type azureAuthResultMsg struct {
	success bool
	token   string
	err     error
}

// Mensagens para Node Pool Management
type nodePoolsLoadedMsg struct {
	nodePools    []models.NodePool
	subscription string
	err          error
	azureLogMsg  *statusLogMsg // Log opcional para Azure operations
}

type nodePoolUpdateMsg struct {
	nodePool models.NodePool
	err      error
}

type nodePoolsConfiguratingSubscriptionMsg struct {
	clusterConfig *models.ClusterConfig
}

type nodePoolsAppliedMsg struct {
	appliedPools []models.NodePool
	err          error
}

// Mensagens para CronJob Management
type cronJobsLoadedMsg struct {
	cronJobs []models.CronJob
	err      error
}

type cronJobUpdateMsg struct {
	cronJob models.CronJob
	err     error
}

// initAzureAuth inicia a autenticação Azure
func (a *App) initAzureAuth() tea.Cmd {
	return func() tea.Msg {
		return azureAuthStartMsg{}
	}
}

// performAzureAuth realiza a autenticação via Azure CLI
func (a *App) performAzureAuth() tea.Cmd {
	return func() tea.Msg {
		fmt.Printf("🔐 Starting Azure CLI authentication...\n")
		fmt.Printf("📱 Your browser will open for Azure login\n")
		
		// Executar az login
		cmd := exec.Command("az", "login")
		
		output, err := cmd.CombinedOutput()
		if err != nil {
			return azureAuthResultMsg{
				success: false,
				err:     fmt.Errorf("Azure CLI login failed: %s - output: %s", err.Error(), string(output)),
			}
		}

		// Verificar se login foi bem-sucedido
		if !isAzureCliAuthenticated() {
			return azureAuthResultMsg{
				success: false,
				err:     fmt.Errorf("Azure CLI authentication verification failed"),
			}
		}

		fmt.Printf("✅ Azure CLI authentication successful\n")
		
		return azureAuthResultMsg{
			success: true,
			token:   "azure-cli-authenticated",
			err:     nil,
		}
	}
}

// loadNodePools carrega node pools do cluster selecionado via Azure CLI
func (a *App) loadNodePools() tea.Cmd {
	return func() tea.Msg {
		if a.model.SelectedCluster == nil {
			return nodePoolsLoadedMsg{err: fmt.Errorf("no cluster selected")}
		}

		// 1. Verificar se estamos logados no Azure CLI
		statusPanel := a.model.StatusContainer
		if err := ensureAzureLoginWithStatus(statusPanel); err != nil {
			return nodePoolsLoadedMsg{err: fmt.Errorf("failed to authenticate with Azure: %w", err)}
		}

		// 2. Buscar configuração do cluster no clusters-config.json
		clusterConfig, err := findClusterInConfig(a.model.SelectedCluster.Name)
		if err != nil {
			return nodePoolsLoadedMsg{err: fmt.Errorf("failed to find cluster in config: %w", err)}
		}

		// 3. Configurar subscription do clusters-config.json
		// Primeiro, retornar a mensagem "Configurando" para permitir atualização da UI
		return nodePoolsConfiguratingSubscriptionMsg{clusterConfig: clusterConfig}
	}
}

// configurateSubscription realiza a configuração da subscription após mostrar a mensagem inicial
func configurateSubscription(clusterConfig *models.ClusterConfig) tea.Cmd {
	return configurateSubscriptionWithStatus(clusterConfig, nil)
}

// configurateSubscriptionWithStatus versão com StatusPanel
func configurateSubscriptionWithStatus(clusterConfig *models.ClusterConfig, statusPanel interface{}) tea.Cmd {
	return func() tea.Msg {
		// Log do início da configuração Azure
		// Nota: Vamos usar um batch de comandos para enviar multiple logs

		// Verificar autenticação Azure primeiro
		if !isAzureCliAuthenticated() {
			return statusLogMsg{
				level:   "error",
				source:  "azure-auth",
				message: "❌ Azure CLI não autenticado. Execute 'az login' primeiro.",
			}
		}

		// Configurar a subscription
		cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
		if err := cmd.Run(); err != nil {
			return nodePoolsLoadedMsg{
				err: fmt.Errorf("failed to set subscription '%s': %w", clusterConfig.Subscription, err),
				azureLogMsg: &statusLogMsg{
					level:   "error",
					source:  "azure-config",
					message: fmt.Sprintf("❌ Falha ao configurar subscription: %s", clusterConfig.Subscription),
				},
			}
		}

		// 4. Normalizar nome do cluster para Azure CLI (remover -admin se existir)
		clusterNameForAzure := clusterConfig.ClusterName
		if strings.HasSuffix(clusterNameForAzure, "-admin") {
			clusterNameForAzure = strings.TrimSuffix(clusterNameForAzure, "-admin")
		}

		// 5. Listar node pools do cluster via Azure CLI usando a função correta com StatusPanel
		nodePools, err := loadNodePoolsFromAzureWithRetryAndStatus(clusterNameForAzure, clusterConfig.ResourceGroup, clusterConfig.Subscription, true, statusPanel)
		if err != nil {
			return nodePoolsLoadedMsg{
				err: fmt.Errorf("failed to load node pools from Azure: %w", err),
				azureLogMsg: &statusLogMsg{
					level:   "error",
					source:  "azure-nodepool",
					message: fmt.Sprintf("❌ Falha ao carregar node pools: %s", err.Error()),
				},
			}
		}

		return nodePoolsLoadedMsg{
			nodePools:   nodePools,
			subscription: clusterConfig.Subscription,
			err:         nil,
			azureLogMsg: &statusLogMsg{
				level:   "success",
				source:  "azure-config",
				message: fmt.Sprintf("✅ Subscription configurada: %s", clusterConfig.Subscription),
			},
		}
	}
}

// Funções auxiliares para Azure CLI integration

// isAzureCliAuthenticated verifica se o Azure CLI está autenticado
func isAzureCliAuthenticated() bool {
	cmd := exec.Command("az", "account", "show")
	err := cmd.Run()
	return err == nil
}

// loadClusterConfig carrega a configuração de clusters do arquivo
func loadClusterConfig() ([]models.ClusterConfig, error) {
	// Procurar o arquivo na mesma pasta do executável
	wd, _ := os.Getwd()
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}
	
	execDir := filepath.Dir(execPath)
	configPath := filepath.Join(execDir, "clusters-config.json")
	
	// Se não existir no diretório do executável, procurar no diretório atual
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(wd, "clusters-config.json")
	}
	
	// Verificar se o arquivo existe
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("clusters-config.json not found. Tried: %s and %s", filepath.Join(execDir, "clusters-config.json"), filepath.Join(wd, "clusters-config.json"))
	}
	
	// Ler o arquivo
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read clusters-config.json: %w", err)
	}
	
	// Parse do JSON
	var clusters []models.ClusterConfig
	if err := json.Unmarshal(data, &clusters); err != nil {
		return nil, fmt.Errorf("failed to parse clusters-config.json: %w", err)
	}
	
	return clusters, nil
}

// setActiveSubscription define a subscription ativa no Azure CLI
func setActiveSubscription(subscription string) error {
	// Usar aspas para nomes com espaços
	cmd := exec.Command("az", "account", "set", "--subscription", subscription)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set subscription '%s': %s - output: %s", subscription, err.Error(), string(output))
	}
	
	return nil
}

// findClusterResourceGroup encontra o resource group e subscription do cluster usando configuração ou Azure CLI
func findClusterResourceGroup(clusterName string) (string, string, error) {
	// Primeiro, tentar carregar do arquivo de configuração
	clusters, err := loadClusterConfig()
	if err != nil {
		// Se não encontrou config, fazer fallback para Azure CLI
	} else {
		// Normalizar nome do cluster removendo sufixo -admin se existir
		normalizedClusterName := clusterName
		if strings.HasSuffix(clusterName, "-admin") {
			normalizedClusterName = strings.TrimSuffix(clusterName, "-admin")
		}
		
		// Procurar o cluster na configuração
		for _, cluster := range clusters {
			if cluster.ClusterName == normalizedClusterName {
				// Verificar se a subscription está ativa
				if err := setActiveSubscription(cluster.Subscription); err != nil {
					// Continuar mesmo se falhar em definir subscription
				}
				
				return cluster.ResourceGroup, cluster.Subscription, nil
			}
		}
	}
	
	// Fallback para busca automática no Azure CLI
	subscriptionsCmd := exec.Command("az", "account", "list", "--output", "json")
	subscriptionsOutput, err := subscriptionsCmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to list subscriptions: %w", err)
	}
	
	var subscriptions []AzureSubscription
	if err := json.Unmarshal(subscriptionsOutput, &subscriptions); err != nil {
		return "", "", fmt.Errorf("failed to parse subscriptions: %w", err)
	}
	
	// Buscar o cluster em todas as subscriptions
	for _, subscription := range subscriptions {
		// Buscar clusters nesta subscription
		cmd := exec.Command("az", "aks", "list", "--subscription", subscription.ID, "--output", "json")
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		
		var clusters []AzureCluster
		if err := json.Unmarshal(output, &clusters); err != nil {
			continue
		}
		
		// Procurar o cluster pelo nome
		for _, cluster := range clusters {
			if cluster.Name == clusterName {
				return cluster.ResourceGroup, subscription.ID, nil
			}
		}
	}
	
	// Se não encontrou em nenhuma subscription, tentar o método de extração do nome
	resourceGroup, err := extractResourceGroupFromCluster(clusterName)
	if err != nil {
		return "", "", err
	}
	// Para o método de extração, não temos subscription, retorna vazio
	return resourceGroup, "", nil
}

// AzureAccount representa a subscription atual do Azure CLI
type AzureAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AzureSubscription representa uma subscription do Azure
type AzureSubscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
	State     string `json:"state"`
}


// AzureCluster representa um cluster AKS retornado pelo Azure CLI
type AzureCluster struct {
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
}

// extractResourceGroupFromCluster extrai o resource group do nome do cluster
func extractResourceGroupFromCluster(clusterName string) (string, error) {
	// Detectar diferentes padrões de nomes de clusters
	
	// Padrão 1: akspriv-<nome>-<ambiente>-admin
	// Resource group: rg-<nome>-app-<ambiente>
	if strings.HasPrefix(clusterName, "akspriv-") && strings.HasSuffix(clusterName, "-admin") {
		// Remove "akspriv-" do início e "-admin" do final
		middle := strings.TrimPrefix(clusterName, "akspriv-")
		middle = strings.TrimSuffix(middle, "-admin")
		
		// Split para pegar nome e ambiente
		parts := strings.Split(middle, "-")
		if len(parts) >= 2 {
			// Últimas partes são ambiente, primeiras são nome
			ambiente := parts[len(parts)-1]
			nome := strings.Join(parts[:len(parts)-1], "-")
			
			resourceGroup := fmt.Sprintf("rg-%s-app-%s", nome, ambiente)
			return resourceGroup, nil
		}
	}
	
	// Padrão 2: nome direto do cluster (ex: "faturamento")
	// Resource group: rg-<nome>-app-prd (assumir prd como padrão)
	if !strings.Contains(clusterName, "-") {
		resourceGroup := fmt.Sprintf("rg-%s-app-prd", clusterName)
		return resourceGroup, nil
	}
	
	// Padrão 3: akspriv-<ambiente>-<region>-<suffix> (padrão antigo)
	if strings.HasPrefix(clusterName, "akspriv-") {
		suffix := strings.TrimPrefix(clusterName, "akspriv-")
		parts := strings.Split(suffix, "-")
		
		if len(parts) >= 2 {
			ambiente := parts[0]
			region := parts[1]
			resourceGroup := fmt.Sprintf("rg-aks-%s-%s", ambiente, region)
			return resourceGroup, nil
		}
	}
	
	return "", fmt.Errorf("unable to determine resource group pattern for cluster: %s", clusterName)
}

// findClusterInConfig busca o cluster no arquivo clusters-config.json
func findClusterInConfig(clusterName string) (*models.ClusterConfig, error) {
	// Normalizar nome do cluster (remover -admin se existir para busca)
	searchName := clusterName
	if strings.HasSuffix(searchName, "-admin") {
		searchName = strings.TrimSuffix(searchName, "-admin")
	}

	// Carregar configurações dos clusters
	configs, err := loadClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster config: %w", err)
	}

	// Buscar o cluster na configuração
	for _, config := range configs {
		if config.ClusterName == searchName {
			return &config, nil
		}
	}

	return nil, fmt.Errorf("cluster '%s' not found in clusters-config.json", searchName)
}

// ensureAzureLogin verifica se estamos logados no Azure CLI e faz login se necessário
func ensureAzureLogin() error {
	return ensureAzureLoginWithStatus(nil)
}

// ensureAzureLoginWithStatus verifica se estamos logados no Azure CLI e faz login se necessário, com status panel
func ensureAzureLoginWithStatus(statusPanel interface{}) error {
	// Verificar primeiro se Azure CLI básico está funcionando
	cmd := exec.Command("az", "account", "show")
	_, err := cmd.CombinedOutput()
	if err != nil {
		// Não estamos logados de forma alguma
		return performAzureLoginWithStatus("not logged in", statusPanel)
	}
	
	// Verificar se temos permissões para AKS com um comando de teste
	// Usamos uma subscription conhecida do clusters-config.json
	testCmd := exec.Command("az", "account", "list-locations", "--output", "json")
	testOutput, testErr := testCmd.CombinedOutput()
	if testErr != nil {
		testOutputStr := string(testOutput)
		if strings.Contains(testOutputStr, "AADSTS50173") ||
		   strings.Contains(testOutputStr, "expired") ||
		   strings.Contains(testOutputStr, "The provided grant has expired") {
			// Token expirado, precisamos reautenticar
			return performAzureLoginWithStatus(testOutputStr, statusPanel)
		}
		// Outro tipo de erro, não relacionado à autenticação
		return fmt.Errorf("Azure CLI test command failed: %w", testErr)
	}
	
	// Tudo funcionando
	return nil
}

// performAzureLogin realiza o processo de login do Azure
func performAzureLogin(errorContext string) error {
	return performAzureLoginWithStatus(errorContext, nil)
}

// logToStatusPanel loga uma mensagem tanto no console quanto no status panel se disponível
func logToStatusPanel(statusPanel interface{}, level, source, message string) {
	// Sempre logar no console para debugging
	fmt.Printf("%s\n", message)

	// Se temos status panel, usar também
	if statusPanel != nil {
		if sp, ok := statusPanel.(interface{
			Info(string, string)
			Success(string, string)
			Warning(string, string)
			Error(string, string)
		}); ok {
			switch level {
			case "info":
				sp.Info(source, message)
			case "success":
				sp.Success(source, message)
			case "warning":
				sp.Warning(source, message)
			case "error":
				sp.Error(source, message)
			default:
				sp.Info(source, message)
			}
		}
	}
}

// performAzureLoginWithStatus realiza o processo de login do Azure com status panel
func performAzureLoginWithStatus(errorContext string, statusPanel interface{}) error {
	// Verificar se é erro de token expirado
	if strings.Contains(errorContext, "AADSTS50173") || strings.Contains(errorContext, "expired") ||
	   strings.Contains(errorContext, "The provided grant has expired") {
		logToStatusPanel(statusPanel, "info", "azure-auth", "🔄 Azure token expired, re-authenticating...")
		
		// Fazer logout primeiro para limpar token expirado
		logoutCmd := exec.Command("az", "logout")
		logoutCmd.Run() // Ignorar erros de logout
		
		// Extrair tenant ID da mensagem de erro se disponível
		tenantID := "5a86b3fb-4213-49cd-b4d6-be91482ad3c0" // Default fallback
		if strings.Contains(errorContext, "--tenant") {
			// Tentar extrair tenant ID da mensagem de erro
			if start := strings.Index(errorContext, "--tenant \""); start != -1 {
				start += len("--tenant \"")
				if end := strings.Index(errorContext[start:], "\""); end != -1 {
					tenantID = errorContext[start : start+end]
				}
			}
		}
		
		// Fazer login com tenant específico
		logToStatusPanel(statusPanel, "info", "azure-auth", fmt.Sprintf("📱 Opening browser for Azure authentication with tenant: %s", tenantID))
		loginCmd := exec.Command("az", "login", "--tenant", tenantID, "--scope", "https://management.core.windows.net//.default")
		loginOutput, loginErr := loginCmd.CombinedOutput()
		if loginErr != nil {
			return fmt.Errorf("failed to re-authenticate with Azure (tenant: %s): %w\nOutput: %s", tenantID, loginErr, string(loginOutput))
		}
		
		logToStatusPanel(statusPanel, "success", "azure-auth", "✅ Azure re-authentication completed")
		return nil
	}

	// Não estamos logados, fazer login inicial via Azure CLI
	logToStatusPanel(statusPanel, "info", "azure-auth", "🔄 Authenticating with Azure CLI...")
	loginCmd := exec.Command("az", "login", "--only-show-errors")
	loginOutput, loginErr := loginCmd.CombinedOutput()
	if loginErr != nil {
		logToStatusPanel(statusPanel, "error", "azure-auth", fmt.Sprintf("❌ Failed to login to Azure: %v", loginErr))
		return fmt.Errorf("failed to login to Azure: %w (output: %s)", loginErr, string(loginOutput))
	}

	logToStatusPanel(statusPanel, "success", "azure-auth", "✅ Azure authentication completed")
	return nil
}

// loadNodePoolsFromAzure carrega node pools via Azure CLI
func loadNodePoolsFromAzure(clusterName, resourceGroup, subscription string) ([]models.NodePool, error) {
	return loadNodePoolsFromAzureWithRetry(clusterName, resourceGroup, subscription, true)
}

// loadNodePoolsFromAzureWithRetry carrega node pools com retry de autenticação
func loadNodePoolsFromAzureWithRetry(clusterName, resourceGroup, subscription string, allowRetry bool) ([]models.NodePool, error) {
	return loadNodePoolsFromAzureWithRetryAndStatus(clusterName, resourceGroup, subscription, allowRetry, nil)
}

// loadNodePoolsFromAzureWithRetryAndStatus carrega node pools com retry de autenticação e StatusPanel
func loadNodePoolsFromAzureWithRetryAndStatus(clusterName, resourceGroup, subscription string, allowRetry bool, statusPanel interface{}) ([]models.NodePool, error) {
	// Executar comando Azure CLI
	cmd := exec.Command("az", "aks", "nodepool", "list",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterName,
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		// Capturar stderr para melhor debugging
		if exitError, ok := err.(*exec.ExitError); ok {
			stderr := string(exitError.Stderr)

			// Verificar se é erro de autenticação e tentar reautenticar
			if allowRetry && (strings.Contains(stderr, "AADSTS50173") ||
				strings.Contains(stderr, "expired") ||
				strings.Contains(stderr, "The provided grant has expired") ||
				strings.Contains(stderr, "authentication")) {

				logToStatusPanel(statusPanel, "info", "azure-auth", "🔄 Authentication error detected, attempting re-authentication...")

				// Tentar reautenticar
				if authErr := ensureAzureLoginWithStatus(statusPanel); authErr != nil {
					return nil, fmt.Errorf("failed to re-authenticate: %w", authErr)
				}

				// Tentar novamente (sem retry para evitar loop infinito)
				logToStatusPanel(statusPanel, "info", "azure-auth", "🔄 Retrying node pool loading after re-authentication...")
				return loadNodePoolsFromAzureWithRetryAndStatus(clusterName, resourceGroup, subscription, false, statusPanel)
			}
			
			return nil, fmt.Errorf("az command failed: %s - stderr: %s", err.Error(), stderr)
		}
		return nil, fmt.Errorf("failed to execute az command: %w", err)
	}
	
	// Parse do JSON
	var azureNodePools []AzureNodePool
	if err := json.Unmarshal(output, &azureNodePools); err != nil {
		return nil, fmt.Errorf("failed to parse Azure CLI output: %w", err)
	}
	
	// Converter para nosso modelo
	var nodePools []models.NodePool
	for _, azPool := range azureNodePools {
		// Converter pointers para valores diretos, usando 0 como padrão se null
		var minCount, maxCount int32
		if azPool.MinCount != nil {
			minCount = *azPool.MinCount
		}
		if azPool.MaxCount != nil {
			maxCount = *azPool.MaxCount
		}

		nodePool := models.NodePool{
			Name:               azPool.Name,
			VMSize:             azPool.VmSize,
			NodeCount:          azPool.Count,
			MinNodeCount:       minCount,
			MaxNodeCount:       maxCount,
			AutoscalingEnabled: azPool.EnableAutoScaling, // Usar campo correto do Azure
			Status:             azPool.ProvisioningState,
			IsSystemPool:       azPool.Mode == "System",
			ClusterName:        clusterName,
			ResourceGroup:      resourceGroup,
			Subscription:       subscription,
			Selected:           false, // Inicializar explicitamente como não selecionado
			Modified:           false, // Inicializar como não modificado
		}

		// Definir valores originais
		nodePool.OriginalValues = models.NodePoolValues{
			NodeCount:          nodePool.NodeCount,
			MinNodeCount:       nodePool.MinNodeCount,
			MaxNodeCount:       nodePool.MaxNodeCount,
			AutoscalingEnabled: nodePool.AutoscalingEnabled,
		}

		nodePools = append(nodePools, nodePool)
	}
	
	return nodePools, nil
}



// AzureNodePool representa a estrutura retornada pela Azure CLI
type AzureNodePool struct {
	Name                    string `json:"name"`
	VmSize                  string `json:"vmSize"`
	Count                   int32  `json:"count"`
	MinCount                *int32 `json:"minCount"`        // Pointer pois pode ser null
	MaxCount                *int32 `json:"maxCount"`        // Pointer pois pode ser null
	EnableAutoScaling       bool   `json:"enableAutoScaling"` // Campo correto do Azure
	Mode                    string `json:"mode"`            // "System" ou "User"
	ProvisioningState       string `json:"provisioningState"`
	ScaleSetEvictionPolicy  string `json:"scaleSetEvictionPolicy"`
}

// clearScreen retorna um comando para forçar redesenho da tela
func clearScreen() tea.Cmd {
	return tea.Batch(
		tea.ClearScreen,
		func() tea.Msg {
			return clearScreenMsg{}
		},
	)
}

// startStatusRefreshTimer inicia timer de auto-refresh para o painel Status
func startStatusRefreshTimer() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return statusRefreshMsg{}
	})
}

