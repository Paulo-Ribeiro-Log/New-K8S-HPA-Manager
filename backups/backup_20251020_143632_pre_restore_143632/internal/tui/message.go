package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/session"
	"k8s-hpa-manager/internal/tui/components"
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

// Mensagens para validação periódica de conectividade
type vpnCheckMsg struct{}
type azureADCheckMsg struct{}

type vpnStatusMsg struct {
	connected bool
	message   string
	err       error
}

type azureADStatusMsg struct {
	authenticated bool
	message       string
	err           error
}

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
	vpnError bool // Flag para indicar erro de VPN (ativa modal)
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
	vpnError   bool // Flag para indicar erro de VPN (ativa modal)
}

// Mensagens para HPAs
type hpasLoadedMsg struct {
	hpas     []models.HPA
	err      error
	vpnError bool // Flag para indicar erro de VPN (ativa modal)
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

		statusPanel := a.model.StatusContainer

		// 1. VALIDAR VPN ANTES DE QUALQUER COISA (kubectl precisa de VPN)
		a.model.StatusContainer.AddInfo("vpn-check", "🔍 Validando conectividade VPN...")
		vpnErr := checkVPNConnectivity(statusPanel)
		if vpnErr != nil {
			a.model.StatusContainer.AddError("vpn-check", "❌ VPN desconectada - kubectl não funcionará")
			// Retornar erro especial que ativa o modal
			return clustersDiscoveredMsg{
				err:      fmt.Errorf("VPN desconectada: %w", vpnErr),
				vpnError: true, // Flag para ativar modal
			}
		}
		a.model.StatusContainer.AddSuccess("vpn-check", "✅ VPN conectada")

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
		cmds = append(cmds, a.testSingleClusterConnection(cluster.Context))
	}

	return tea.Batch(cmds...)
}

// testSingleClusterConnection testa conexão com um cluster específico usando o context name
func (a *App) testSingleClusterConnection(contextName string) tea.Cmd {
	return func() tea.Msg {
		if a.kubeManager == nil {
			return clusterConnectionTestMsg{
				cluster: contextName,
				status:  models.StatusError,
				err:     fmt.Errorf("kube manager not initialized"),
			}
		}

		// Testar conexão real com o cluster usando o context name
		status := a.kubeManager.TestClusterConnection(a.ctx, contextName)

		// Se falhou/timeout, diagnosticar conectividade (VPN/Azure AD)
		if status == models.StatusTimeout || status == models.StatusError {
			a.debugLog("⚠️ Cluster %s timeout/erro - iniciando diagnóstico", contextName)
			// Não bloquear - diagnóstico roda em background
			go func() {
				a.diagnoseConnectivityIssue(fmt.Sprintf("cluster %s connection failed", contextName))
			}()
		}

		return clusterConnectionTestMsg{
			cluster: contextName,
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

// Mensagens para execução sequencial de node pools (stress test)
type sequentialNodePoolStartMsg struct {
	nodePool models.NodePool
	order    int // 1 ou 2
}

type sequentialNodePoolProgressMsg struct {
	nodePoolName string
	order        int
	progress     int // 0-100
	status       string
}

type sequentialNodePoolCompletedMsg struct {
	nodePoolName string
	order        int
	success      bool
	err          error
}

type sequentialExecutionCheckMsg struct{}

// Mensagens para CronJob Management
type cronJobsLoadedMsg struct {
	cronJobs []models.CronJob
	err      error
}

type cronJobUpdateMsg struct {
	cronJob models.CronJob
	err     error
}

type autoDiscoverResultMsg struct {
	success       bool
	clustersFound int
	errors        []error
	err           error
}

type autoDiscoverLogMsg struct {
	message string
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
		a.model.StatusContainer.AddInfo("azure-auth", "🔐 Starting Azure CLI authentication...")
		a.model.StatusContainer.AddInfo("azure-auth", "📱 Your browser will open for Azure login")
		
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

		a.model.StatusContainer.AddSuccess("azure-auth", "✅ Azure CLI authentication successful")
		
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

		// 1. Buscar configuração do cluster no clusters-config.json
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

		// Configurar a subscription com timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "az", "account", "set", "--subscription", clusterConfig.Subscription)
		err := cmd.Run()

		// Se timeout ou erro de rede, diagnosticar conectividade
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				logToStatusPanel(statusPanel, "warn", "azure-timeout", "⏱️ Timeout ao configurar Azure subscription - diagnosticando...")
				// Diagnosticar de forma síncrona para ter resultado imediato
				diagnoseErr := checkVPNConnectivity(statusPanel)
				if diagnoseErr != nil {
					return nodePoolsLoadedMsg{
						err: fmt.Errorf("VPN desconectada ao configurar subscription: %w", diagnoseErr),
						azureLogMsg: &statusLogMsg{
							level:   "error",
							source:  "vpn-check",
							message: "❌ VPN desconectada - conecte-se à VPN e tente novamente",
						},
					}
				}
			}

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
		clusterNameForAzure := strings.TrimSuffix(clusterConfig.ClusterName, "-admin")

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
	// 1. Procurar primeiro no diretório padrão ~/.k8s-hpa-manager/ (onde autodiscover salva)
	homeConfigPath := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager", "clusters-config.json")
	configPath := homeConfigPath

	// Se existir no diretório padrão, usar ele
	if _, err := os.Stat(homeConfigPath); err == nil {
		// Arquivo encontrado no diretório padrão
	} else {
		// 2. Fallback: procurar no diretório do executável
		execPath, execErr := os.Executable()
		if execErr == nil {
			execDir := filepath.Dir(execPath)
			execConfigPath := filepath.Join(execDir, "clusters-config.json")

			if _, err := os.Stat(execConfigPath); err == nil {
				configPath = execConfigPath
			} else {
				// 3. Último fallback: diretório de trabalho atual
				wd, _ := os.Getwd()
				wdConfigPath := filepath.Join(wd, "clusters-config.json")

				if _, err := os.Stat(wdConfigPath); err == nil {
					configPath = wdConfigPath
				} else {
					return nil, fmt.Errorf("clusters-config.json not found. Tried:\n  1. %s (default)\n  2. %s (exec dir)\n  3. %s (working dir)\n\nRun 'k8s-hpa-manager autodiscover' to generate the config file", homeConfigPath, execConfigPath, wdConfigPath)
				}
			}
		}
	}

	// Verificar novamente se o arquivo existe no caminho escolhido
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("clusters-config.json not found at %s", configPath)
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
	searchName := strings.TrimSuffix(clusterName, "-admin")

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

// checkVPNConnectivity verifica conectividade com Kubernetes (requer VPN) usando kubectl
func checkVPNConnectivity(statusPanel interface{}) error {
	// Buscar clusters de produção e homologação do kubeconfig para double check
	var prdContext, hlgContext string

	// Obter lista de contextos do kubeconfig
	cmd := exec.Command("kubectl", "config", "get-contexts", "-o", "name")
	output, err := cmd.Output()
	if err == nil {
		contexts := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, context := range contexts {
			// Buscar primeiro contexto -prd* (com ou sem sufixo)
			if strings.Contains(context, "-prd") && prdContext == "" {
				prdContext = context
			}
			// Buscar primeiro contexto -hlg* (com ou sem sufixo)
			if strings.Contains(context, "-hlg") && hlgContext == "" {
				hlgContext = context
			}
		}
	}

	// Tentar primeiro o contexto atual
	if err := testKubernetesConnectivity("", statusPanel); err == nil {
		return nil // Contexto atual funcionou
	}

	// Se contexto atual falhou, tentar produção
	if prdContext != "" {
		if sp, ok := statusPanel.(*components.StatusContainer); ok {
			sp.AddInfo("vpn-check", "🔄 Tentando cluster produção para validação...")
		}
		if err := testKubernetesConnectivity(prdContext, statusPanel); err == nil {
			if sp, ok := statusPanel.(*components.StatusContainer); ok {
				sp.AddSuccess("vpn-check", "✅ VPN conectada - Validado via cluster produção")
			}
			return nil
		}
	}

	// Se produção falhou, tentar homologação
	if hlgContext != "" {
		if sp, ok := statusPanel.(*components.StatusContainer); ok {
			sp.AddInfo("vpn-check", "🔄 Tentando cluster homologação para validação...")
		}
		if err := testKubernetesConnectivity(hlgContext, statusPanel); err == nil {
			if sp, ok := statusPanel.(*components.StatusContainer); ok {
				sp.AddWarning("vpn-check", "⚠️ VPN conectada mas cluster atual pode estar offline")
			}
			return nil
		}
	}

	// Todos falharam - VPN desconectada
	if sp, ok := statusPanel.(*components.StatusContainer); ok {
		sp.AddError("vpn-check", "❌ VPN desconectada - Nenhum cluster acessível")
	}
	return fmt.Errorf("VPN desconectada: nenhum cluster acessível (atual, prd, hlg)")
}

// testKubernetesConnectivity testa conectividade com um contexto específico
func testKubernetesConnectivity(context string, statusPanel interface{}) error {
	var testCmd *exec.Cmd
	if context != "" {
		testCmd = exec.Command("kubectl", "cluster-info", "--context", context, "--request-timeout=5s")
	} else {
		testCmd = exec.Command("kubectl", "cluster-info", "--request-timeout=5s")
	}

	// Criar canal para timeout
	done := make(chan error, 1)

	go func() {
		output, err := testCmd.CombinedOutput()
		outputStr := string(output)

		// Se kubectl conseguiu responder (mesmo que seja erro de auth), VPN está OK
		// Procurar por "Kubernetes control plane" ou "running at" na saída
		if err == nil || strings.Contains(outputStr, "running at") || strings.Contains(outputStr, "Kubernetes") {
			done <- nil
		} else {
			done <- fmt.Errorf("kubectl falhou: %w (output: %s)", err, outputStr)
		}
	}()

	// Timeout de 6 segundos para detectar VPN desconectada
	select {
	case err := <-done:
		return err
	case <-time.After(6 * time.Second):
		// Timeout
		testCmd.Process.Kill()
		return fmt.Errorf("timeout ao acessar Kubernetes")
	}
}

// Cache global de validação Azure (thread-safe)
var (
	azureAuthCache struct {
		sync.RWMutex
		isAuthenticated bool
		lastCheck       time.Time
		validUntil      time.Time
		inProgress      bool
	}
)

// ensureAzureLoginWithStatus verifica se estamos logados no Azure CLI e faz login se necessário, com status panel
func ensureAzureLoginWithStatus(statusPanel interface{}) error {
	// 1. Verificar cache primeiro (evita múltiplas validações simultâneas)
	azureAuthCache.RLock()
	if azureAuthCache.isAuthenticated && time.Now().Before(azureAuthCache.validUntil) {
		// Cache válido - retornar imediatamente
		azureAuthCache.RUnlock()
		logToStatusPanel(statusPanel, "success", "azure-auth", "✅ Azure autenticado (cache)")
		return nil
	}

	// Se outra goroutine está validando, aguardar
	if azureAuthCache.inProgress {
		azureAuthCache.RUnlock()
		logToStatusPanel(statusPanel, "info", "azure-auth", "⏳ Aguardando validação Azure...")

		// Aguardar até 10 segundos pela validação em progresso
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			azureAuthCache.RLock()
			if !azureAuthCache.inProgress {
				isAuth := azureAuthCache.isAuthenticated
				azureAuthCache.RUnlock()
				if isAuth {
					return nil
				}
				break
			}
			azureAuthCache.RUnlock()
		}
	} else {
		azureAuthCache.RUnlock()
	}

	// 2. Adquirir lock para validação (evitar validações concorrentes)
	azureAuthCache.Lock()

	// Verificar novamente se não foi validado enquanto aguardávamos o lock
	if azureAuthCache.isAuthenticated && time.Now().Before(azureAuthCache.validUntil) {
		azureAuthCache.Unlock()
		return nil
	}

	azureAuthCache.inProgress = true
	azureAuthCache.Unlock()

	// Garantir que inProgress seja resetado ao final
	defer func() {
		azureAuthCache.Lock()
		azureAuthCache.inProgress = false
		azureAuthCache.Unlock()
	}()

	logToStatusPanel(statusPanel, "info", "azure-auth", "🔐 Verificando autenticação Azure...")

	// 3. Verificar se Azure CLI está autenticado (sem timeout - quick check)
	showCmd := exec.Command("az", "account", "show", "--output", "json")
	showOutput, showErr := showCmd.CombinedOutput()

	if showErr != nil {
		azureAuthCache.Lock()
		azureAuthCache.isAuthenticated = false
		azureAuthCache.lastCheck = time.Now()
		azureAuthCache.Unlock()

		logToStatusPanel(statusPanel, "error", "azure-auth", "❌ Azure CLI não autenticado")
		return performAzureLoginWithStatus("not logged in", statusPanel)
	}

	// 4. Parsear informações da conta
	var accountInfo map[string]interface{}
	if err := json.Unmarshal(showOutput, &accountInfo); err != nil {
		azureAuthCache.Lock()
		azureAuthCache.isAuthenticated = false
		azureAuthCache.lastCheck = time.Now()
		azureAuthCache.Unlock()

		logToStatusPanel(statusPanel, "error", "azure-auth", "❌ Erro ao parsear Azure CLI")
		return fmt.Errorf("failed to parse Azure CLI output: %w", err)
	}

	// 5. Verificar expiração do token (sem chamada de API pesada)
	// Se o token tem menos de 30 minutos, consideramos válido
	now := time.Now()
	cacheValidUntil := now.Add(30 * time.Minute)

	// Atualizar cache com sucesso
	azureAuthCache.Lock()
	azureAuthCache.isAuthenticated = true
	azureAuthCache.lastCheck = now
	azureAuthCache.validUntil = cacheValidUntil
	azureAuthCache.Unlock()

	logToStatusPanel(statusPanel, "success", "azure-auth", "✅ Autenticação Azure OK")
	return nil
}

// performAzureLogin realiza o processo de login do Azure
func performAzureLogin(errorContext string) error {
	return performAzureLoginWithStatus(errorContext, nil)
}

// logToStatusPanel loga uma mensagem tanto no console quanto no status panel se disponível
func logToStatusPanel(statusPanel interface{}, level, source, message string) {
	// Logar APENAS no StatusContainer (não no console - isso quebra a TUI)
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
	// Invalidar cache antes de reautenticar
	azureAuthCache.Lock()
	azureAuthCache.isAuthenticated = false
	azureAuthCache.lastCheck = time.Now()
	azureAuthCache.validUntil = time.Time{} // Zero time
	azureAuthCache.Unlock()

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

		// Atualizar cache após login bem-sucedido
		azureAuthCache.Lock()
		azureAuthCache.isAuthenticated = true
		azureAuthCache.lastCheck = time.Now()
		azureAuthCache.validUntil = time.Now().Add(30 * time.Minute)
		azureAuthCache.Unlock()

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

	// Atualizar cache após login bem-sucedido
	azureAuthCache.Lock()
	azureAuthCache.isAuthenticated = true
	azureAuthCache.lastCheck = time.Now()
	azureAuthCache.validUntil = time.Now().Add(30 * time.Minute)
	azureAuthCache.Unlock()

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

				// Invalidar cache antes de reautenticar
				azureAuthCache.Lock()
				azureAuthCache.isAuthenticated = false
				azureAuthCache.validUntil = time.Time{}
				azureAuthCache.Unlock()

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

// startVPNCheckTimer inicia timer de validação periódica de VPN (a cada 30 segundos)
func startVPNCheckTimer() tea.Cmd {
	return tea.Tick(time.Second*30, func(t time.Time) tea.Msg {
		return vpnCheckMsg{}
	})
}

// startAzureADCheckTimer inicia timer de validação periódica de Azure AD (a cada 5 minutos)
func startAzureADCheckTimer() tea.Cmd {
	return tea.Tick(time.Minute*5, func(t time.Time) tea.Msg {
		return azureADCheckMsg{}
	})
}

// invalidateAzureCache invalida o cache de autenticação Azure
func invalidateAzureCache() {
	azureAuthCache.Lock()
	azureAuthCache.isAuthenticated = false
	azureAuthCache.validUntil = time.Time{}
	azureAuthCache.lastCheck = time.Time{}
	azureAuthCache.Unlock()
}

// diagnoseConnectivityIssue diagnostica problemas de conectividade (VPN e Azure AD)
// Chamado automaticamente quando há timeout/falha de conexão
func (a *App) diagnoseConnectivityIssue(errorContext string) tea.Cmd {
	return func() tea.Msg {
		statusPanel := a.model.StatusContainer

		logToStatusPanel(statusPanel, "warning", "diagnostic", "⚠️ Timeout detectado - diagnosticando...")

		// 1. Verificar VPN primeiro
		logToStatusPanel(statusPanel, "info", "diagnostic", "🔍 1/2: Testando VPN...")
		vpnErr := checkVPNConnectivity(statusPanel)

		if vpnErr != nil {
			// VPN é o problema!
			logToStatusPanel(statusPanel, "error", "diagnostic", "❌ DIAGNÓSTICO: VPN desconectada")
			logToStatusPanel(statusPanel, "info", "diagnostic", "💡 SOLUÇÃO: Conecte-se à VPN e tente novamente (F5)")

			a.model.VPNConnected = false
			a.model.VPNStatusMessage = "VPN Desconectada"
			a.model.VPNLastCheck = time.Now()

			// Invalidar cache Azure também (VPN requer reautenticação)
			invalidateAzureCache()

			return vpnStatusMsg{
				connected: false,
				message:   "VPN Desconectada",
				err:       vpnErr,
			}
		}

		// VPN OK, verificar Azure AD
		logToStatusPanel(statusPanel, "success", "diagnostic", "✅ VPN OK")
		a.model.VPNConnected = true
		a.model.VPNStatusMessage = "VPN Conectada"
		a.model.VPNLastCheck = time.Now()

		logToStatusPanel(statusPanel, "info", "diagnostic", "🔍 2/2: Testando Azure AD...")

		// Invalidar cache para forçar revalidação
		invalidateAzureCache()

		// Verificar Azure AD (força revalidação sem cache)
		authErr := ensureAzureLoginWithStatus(statusPanel)

		if authErr != nil {
			logToStatusPanel(statusPanel, "error", "diagnostic", "❌ DIAGNÓSTICO: Azure AD token expirado/inválido")
			logToStatusPanel(statusPanel, "info", "diagnostic", "💡 SOLUÇÃO: Execute 'az login' ou aguarde reautenticação automática")

			a.model.AzureADAuthenticated = false
			a.model.AzureADStatusMessage = "Token expirado"
			a.model.AzureADLastCheck = time.Now()

			return azureADStatusMsg{
				authenticated: false,
				message:       "Azure AD: Token expirado",
				err:           authErr,
			}
		}

		logToStatusPanel(statusPanel, "success", "diagnostic", "✅ Azure AD OK")
		logToStatusPanel(statusPanel, "success", "diagnostic", "✅ DIAGNÓSTICO: Conectividade OK")

		a.model.AzureADAuthenticated = true
		a.model.AzureADStatusMessage = "Autenticado"
		a.model.AzureADLastCheck = time.Now()

		return azureADStatusMsg{
			authenticated: true,
			message:       "Azure AD: Autenticado",
			err:           nil,
		}
	}
}

// applySequentialNodePool aplica um node pool de forma assíncrona (para execução sequencial)
func (a *App) applySequentialNodePool(nodePool models.NodePool, order int) tea.Cmd {
	return func() tea.Msg {
		a.debugLog("🚀 Iniciando aplicação assíncrona do node pool %s (ordem %d)", nodePool.Name, order)

		// Atualizar StatusContainer
		a.model.StatusContainer.AddInfo(
			fmt.Sprintf("seq-%d", order),
			fmt.Sprintf("🔄 *%d: Aplicando %s...", order, nodePool.Name),
		)

		// Verificar conectividade VPN antes de aplicar
		if err := checkVPNConnectivity(a.model.StatusContainer); err != nil {
			a.debugLog("❌ Erro de conectividade VPN: %v", err)
			a.model.StatusContainer.AddError(
				fmt.Sprintf("seq-%d", order),
				fmt.Sprintf("❌ *%d: VPN desconectada - %s", order, nodePool.Name),
			)

			return sequentialNodePoolCompletedMsg{
				nodePoolName: nodePool.Name,
				order:        order,
				success:      false,
				err:          fmt.Errorf("VPN desconectada: %w", err),
			}
		}

		// Aplicar mudanças via Azure CLI
		err := a.updateNodePoolViaAzureCLI(nodePool)

		if err != nil {
			a.debugLog("❌ Erro ao aplicar node pool %s: %v", nodePool.Name, err)
			a.model.StatusContainer.AddError(
				fmt.Sprintf("seq-%d", order),
				fmt.Sprintf("❌ *%d: Falhou %s - %v", order, nodePool.Name, err),
			)

			return sequentialNodePoolCompletedMsg{
				nodePoolName: nodePool.Name,
				order:        order,
				success:      false,
				err:          err,
			}
		}

		a.debugLog("✅ Node pool %s aplicado com sucesso", nodePool.Name)
		a.model.StatusContainer.AddSuccess(
			fmt.Sprintf("seq-%d", order),
			fmt.Sprintf("✅ *%d: Completado %s", order, nodePool.Name),
		)

		return sequentialNodePoolCompletedMsg{
			nodePoolName: nodePool.Name,
			order:        order,
			success:      true,
			err:          nil,
		}
	}
}

// startSequentialExecutionMonitor inicia monitoramento de execução sequencial
func startSequentialExecutionMonitor() tea.Cmd {
	return tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
		return sequentialExecutionCheckMsg{}
	})
}

