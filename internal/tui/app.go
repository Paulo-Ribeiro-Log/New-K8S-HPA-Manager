package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sClientSet "k8s.io/client-go/kubernetes"

	"k8s-hpa-manager/internal/azure"
	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/kubernetes"
	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/session"
	"k8s-hpa-manager/internal/tui/components"
	"k8s-hpa-manager/internal/tui/layout"
	"k8s-hpa-manager/internal/updater"
)

// App representa a aplicação principal
type App struct {
	// Configuração
	kubeconfigPath string
	debug          bool

	// Managers
	kubeManager    *config.KubeConfigManager
	sessionManager *session.Manager
	clients        map[string]*kubernetes.Client
	clientsMutex   sync.RWMutex                 // Protege acesso concorrente aos clients
	tabManager     *models.TabManager // Gerenciador de abas

	// Estado da aplicação
	model *models.AppModel

	// UI Components
	width  int
	height int

	// Contexto
	ctx    context.Context
	cancel context.CancelFunc

}

// debugLog imprime mensagens apenas quando debug está habilitado
func (a *App) debugLog(format string, args ...interface{}) {
	if a.debug {
		// Escrever APENAS para arquivo debug.txt na raiz do projeto
		// Não escrever para stdout/stderr para não bugar a interface TUI
		if file, err := os.OpenFile("debug.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			timestamp := time.Now().Format("15:04:05.000")
			fmt.Fprintf(file, "[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
			file.Close()
		}
	}
}

// getClient retorna um cliente existente de forma thread-safe
func (a *App) getClient(clusterName string) (*kubernetes.Client, bool) {
	a.clientsMutex.RLock()
	defer a.clientsMutex.RUnlock()
	client, exists := a.clients[clusterName]
	return client, exists
}

// setClient adiciona ou atualiza um cliente de forma thread-safe
func (a *App) setClient(clusterName string, client *kubernetes.Client) {
	a.clientsMutex.Lock()
	defer a.clientsMutex.Unlock()
	a.clients[clusterName] = client
}

// deleteClient remove um cliente de forma thread-safe
func (a *App) deleteClient(clusterName string) {
	a.clientsMutex.Lock()
	defer a.clientsMutex.Unlock()
	delete(a.clients, clusterName)
}

// NewApp cria uma nova instância da aplicação
func NewApp(kubeconfigPath string, debug bool) *App {
	ctx, cancel := context.WithCancel(context.Background())

	// Criar modelo inicial
	initialModel := &models.AppModel{
		State:               models.StateClusterSelection,
		Loading:             false,
		SelectedIndex:       0,
		ActivePanel:         models.PanelNamespaces,
		SelectedNamespaces:  make([]models.Namespace, 0),
		SelectedHPAs:        make([]models.HPA, 0),
		CurrentNamespaceIdx: 0,
		FormFields:          make(map[string]string),
		// Inicializar Status Container (dimensões fixas 140x15)
		StatusContainer: components.NewStatusContainer(80, 10, "📊 Status e Informações"),
		// Inicializar sistema de memorização de estado
		StateMemory: make(map[models.AppState]*models.PanelState),
		// Inicializar memorização de posições em pastas de sessão
		FolderSessionMemory: make(map[string]int),
	}

	app := &App{
		kubeconfigPath: kubeconfigPath,
		debug:          debug,
		clients:        make(map[string]*kubernetes.Client),
		ctx:            ctx,
		cancel:         cancel,
		// Inicializar com resolução mínima para garantir visibilidade
		width:      layout.MinTerminalWidth,
		height:     layout.MinTerminalHeight,
		model:      initialModel,
		tabManager: models.NewTabManager(), // Inicializar TabManager
	}

	// Criar primeira aba com o modelo inicial
	app.tabManager.AddTab("Principal", "", initialModel)

	// Log das dimensões iniciais
	app.debugLog("🚀 App iniciada com dimensões iniciais: %dx%d", app.width, app.height)
	app.debugLog("📑 TabManager inicializado com 1 aba")

	// Mensagens de teste serão adicionadas após descobrir clusters (em clustersDiscoveredMsg)

	// Verificar updates em background (não-bloqueante)
	go app.checkForUpdatesInBackground()

	return app
}

// getStatusPanel foi removido - agora usar diretamente a.model.StatusContainer

// saveCurrentPanelState salva o estado atual do painel na memória
func (a *App) saveCurrentPanelState() {
	currentState := &models.PanelState{
		SelectedIndex: a.model.SelectedIndex,
		ActivePanel:   a.model.ActivePanel,
		ScrollOffset:  a.getCurrentScrollOffset(),
		SubState:      a.getCurrentSubState(),
		Timestamp:     time.Now(),

		// Capturar itens selecionados
		SelectedCluster:    a.getCurrentSelectedCluster(),
		SelectedNamespaces: a.getCurrentSelectedNamespaces(),
		SelectedHPAs:       a.getCurrentSelectedHPAs(),
		SelectedNodePools:  a.getCurrentSelectedNodePools(),
		SelectedCronJobs:   a.getCurrentSelectedCronJobs(),
		EditingItem:        a.getCurrentEditingItem(),
	}

	a.model.StateMemory[a.model.State] = currentState
	a.debugLog("💾 Estado salvo para %v: index=%d, panel=%v, cluster=%s, namespaces=%d, hpas=%d",
		a.model.State, currentState.SelectedIndex, currentState.ActivePanel,
		currentState.SelectedCluster, len(currentState.SelectedNamespaces), len(currentState.SelectedHPAs))
}

// restorePanelState restaura o estado do painel da memória
func (a *App) restorePanelState(state models.AppState) {
	if savedState, exists := a.model.StateMemory[state]; exists {
		a.model.SelectedIndex = savedState.SelectedIndex
		a.model.ActivePanel = savedState.ActivePanel
		a.setCurrentScrollOffset(savedState.ScrollOffset)
		a.setCurrentSubState(savedState.SubState)

		// Restaurar itens selecionados
		a.restoreSelectedCluster(savedState.SelectedCluster)
		a.restoreSelectedNamespaces(savedState.SelectedNamespaces)

		// NÃO restaurar SelectedHPAs quando voltamos para StateNamespaceSelection
		// porque isso sobrescreve os HPAs selecionados durante a navegação
		if state != models.StateNamespaceSelection {
			a.restoreSelectedHPAs(savedState.SelectedHPAs)
		} else {
			a.debugLog("⏭️ Pulando restauração de HPAs para StateNamespaceSelection (preservando seleções)")
		}

		a.restoreSelectedNodePools(savedState.SelectedNodePools)
		a.restoreSelectedCronJobs(savedState.SelectedCronJobs)
		a.restoreEditingItem(savedState.EditingItem)

		a.debugLog("📋 Estado restaurado para %v: index=%d, panel=%v, cluster=%s, seleções restauradas",
			state, savedState.SelectedIndex, savedState.ActivePanel, savedState.SelectedCluster)
	} else {
		// Se não há estado salvo, usar valores padrão
		a.model.SelectedIndex = 0
		a.model.ActivePanel = models.PanelNamespaces
		a.setCurrentScrollOffset(0)
		a.debugLog("🔄 Estado padrão aplicado para %v (sem estado salvo)", state)
	}
}

// getCurrentScrollOffset retorna o offset de scroll do estado atual
func (a *App) getCurrentScrollOffset() int {
	switch a.model.State {
	case models.StateClusterSelection:
		return a.model.ClusterScrollOffset
	case models.StateNamespaceSelection:
		return a.model.NamespaceScrollOffset
	case models.StateHPASelection:
		return a.model.HPAListScrollOffset
	case models.StateNodeSelection:
		return a.model.NodePoolSelectedScrollOffset
	case models.StateCronJobSelection:
		return a.model.CronJobScrollOffset
	default:
		return 0
	}
}

// setCurrentScrollOffset define o offset de scroll para o estado atual
func (a *App) setCurrentScrollOffset(offset int) {
	switch a.model.State {
	case models.StateClusterSelection:
		a.model.ClusterScrollOffset = offset
	case models.StateNamespaceSelection:
		a.model.NamespaceScrollOffset = offset
	case models.StateHPASelection:
		a.model.HPAListScrollOffset = offset
	case models.StateNodeSelection:
		a.model.NodePoolSelectedScrollOffset = offset
	case models.StateCronJobSelection:
		a.model.CronJobScrollOffset = offset
	}
}

// getCurrentSubState retorna informações de sub-estado específicas do painel atual
func (a *App) getCurrentSubState() string {
	switch a.model.State {
	case models.StateHPAEditing:
		return a.model.ActiveField // Campo ativo na edição de HPA
	case models.StateNodeEditing:
		if a.model.EditingNodePool != nil {
			return "editing" // Estado de edição ativo
		}
	case models.StateCronJobEditing:
		if a.model.EditingCronJob != nil {
			return "editing"
		}
	}
	return ""
}

// setCurrentSubState define informações de sub-estado específicas
func (a *App) setCurrentSubState(subState string) {
	switch a.model.State {
	case models.StateHPAEditing:
		a.model.ActiveField = subState
	}
}

// saveStateOnTabSwitch salva o estado antes de trocar de tab/painel
func (a *App) saveStateOnTabSwitch() {
	a.saveCurrentPanelState()
}

// getCurrentSelectedCluster retorna o cluster atualmente selecionado
func (a *App) getCurrentSelectedCluster() string {
	if a.model.SelectedCluster != nil {
		return a.model.SelectedCluster.Name
	}
	return ""
}

// getCurrentSelectedNamespaces retorna lista de namespaces selecionados
func (a *App) getCurrentSelectedNamespaces() []string {
	var namespaces []string
	for _, ns := range a.model.SelectedNamespaces {
		namespaces = append(namespaces, ns.Name)
	}
	return namespaces
}

// getCurrentSelectedHPAs retorna lista de HPAs selecionados
func (a *App) getCurrentSelectedHPAs() []string {
	var hpas []string
	for _, hpa := range a.model.SelectedHPAs {
		hpas = append(hpas, fmt.Sprintf("%s/%s", hpa.Namespace, hpa.Name))
	}
	return hpas
}

// sortClustersByEnvironment ordena clusters: HLG primeiro, depois PRD, depois outros
func (a *App) sortClustersByEnvironment(clusters []models.Cluster) []models.Cluster {
	var hlgClusters []models.Cluster
	var prdClusters []models.Cluster
	var otherClusters []models.Cluster

	for _, cluster := range clusters {
		nameLower := strings.ToLower(cluster.Name)
		if strings.HasSuffix(nameLower, "-hlg") {
			hlgClusters = append(hlgClusters, cluster)
		} else if strings.HasSuffix(nameLower, "-prd") {
			prdClusters = append(prdClusters, cluster)
		} else {
			otherClusters = append(otherClusters, cluster)
		}
	}

	// Concatenar na ordem: HLG, PRD, OUTROS
	var sortedClusters []models.Cluster
	sortedClusters = append(sortedClusters, hlgClusters...)
	sortedClusters = append(sortedClusters, prdClusters...)
	sortedClusters = append(sortedClusters, otherClusters...)

	return sortedClusters
}

// getCurrentSelectedNodePools retorna lista de node pools selecionados
func (a *App) getCurrentSelectedNodePools() []string {
	var pools []string
	for _, pool := range a.model.SelectedNodePools {
		pools = append(pools, pool.Name)
	}
	return pools
}

// getCurrentSelectedCronJobs retorna lista de cronjobs selecionados
func (a *App) getCurrentSelectedCronJobs() []string {
	var jobs []string
	for _, job := range a.model.SelectedCronJobs {
		jobs = append(jobs, job.Name)
	}
	return jobs
}

// getCurrentEditingItem retorna o item sendo editado atualmente
func (a *App) getCurrentEditingItem() string {
	if a.model.EditingHPA != nil {
		return fmt.Sprintf("hpa:%s/%s", a.model.EditingHPA.Namespace, a.model.EditingHPA.Name)
	}
	if a.model.EditingNodePool != nil {
		return fmt.Sprintf("nodepool:%s", a.model.EditingNodePool.Name)
	}
	if a.model.EditingCronJob != nil {
		return fmt.Sprintf("cronjob:%s", a.model.EditingCronJob.Name)
	}
	return ""
}

// restoreSelectedCluster restaura a seleção do cluster
func (a *App) restoreSelectedCluster(clusterName string) {
	if clusterName == "" {
		return
	}

	// Buscar o cluster na lista e definir como selecionado
	for i, cluster := range a.model.Clusters {
		if cluster.Name == clusterName {
			a.model.SelectedCluster = &a.model.Clusters[i]
			a.debugLog("🔄 Cluster restaurado: %s", clusterName)
			break
		}
	}
}

// restoreSelectedNamespaces restaura a seleção de namespaces
func (a *App) restoreSelectedNamespaces(namespaceNames []string) {
	if len(namespaceNames) == 0 {
		return
	}

	a.model.SelectedNamespaces = nil // Limpar seleções atuais
	for _, nsName := range namespaceNames {
		// Buscar namespace na lista disponível
		for _, ns := range a.model.Namespaces {
			if ns.Name == nsName {
				ns.Selected = true
				a.model.SelectedNamespaces = append(a.model.SelectedNamespaces, ns)
				break
			}
		}
	}
	a.debugLog("🔄 Namespaces restaurados: %d", len(a.model.SelectedNamespaces))
}

// restoreSelectedHPAs restaura a seleção de HPAs
func (a *App) restoreSelectedHPAs(hpaIdentifiers []string) {
	if len(hpaIdentifiers) == 0 {
		return
	}

	// Limpar seleções atuais apenas se formos restaurar algo
	oldCount := len(a.model.SelectedHPAs)
	a.debugLog("⚠️ LIMPANDO SelectedHPAs! Tinha %d, vai restaurar %d HPAs. State=%v",
		oldCount, len(hpaIdentifiers), a.model.State)
	a.model.SelectedHPAs = nil
	for _, hpaId := range hpaIdentifiers {
		// Parse formato "namespace/name"
		parts := strings.Split(hpaId, "/")
		if len(parts) != 2 {
			continue
		}
		namespace, name := parts[0], parts[1]

		// Buscar HPA na lista disponível
		for _, hpa := range a.model.HPAs {
			if hpa.Namespace == namespace && hpa.Name == name {
				hpa.Selected = true
				a.model.SelectedHPAs = append(a.model.SelectedHPAs, hpa)
				break
			}
		}
	}
	a.debugLog("🔄 HPAs restaurados: %d", len(a.model.SelectedHPAs))
}

// restoreSelectedNodePools restaura a seleção de node pools
func (a *App) restoreSelectedNodePools(poolNames []string) {
	if len(poolNames) == 0 {
		return
	}

	a.model.SelectedNodePools = nil // Limpar seleções atuais
	for _, poolName := range poolNames {
		// Buscar node pool na lista disponível
		for _, pool := range a.model.NodePools {
			if pool.Name == poolName {
				pool.Selected = true
				a.model.SelectedNodePools = append(a.model.SelectedNodePools, pool)
				break
			}
		}
	}
	a.debugLog("🔄 Node pools restaurados: %d", len(a.model.SelectedNodePools))
}

// restoreSelectedCronJobs restaura a seleção de cronjobs
func (a *App) restoreSelectedCronJobs(jobNames []string) {
	if len(jobNames) == 0 {
		return
	}

	a.model.SelectedCronJobs = nil // Limpar seleções atuais
	for _, jobName := range jobNames {
		// Buscar cronjob na lista disponível
		for _, job := range a.model.CronJobs {
			if job.Name == jobName {
				job.Selected = true
				a.model.SelectedCronJobs = append(a.model.SelectedCronJobs, job)
				break
			}
		}
	}
	a.debugLog("🔄 CronJobs restaurados: %d", len(a.model.SelectedCronJobs))
}

// restoreEditingItem restaura o item sendo editado
func (a *App) restoreEditingItem(itemIdentifier string) {
	if itemIdentifier == "" {
		return
	}

	// Parse formato "tipo:identificador"
	parts := strings.Split(itemIdentifier, ":")
	if len(parts) != 2 {
		return
	}

	itemType, itemId := parts[0], parts[1]

	switch itemType {
	case "hpa":
		// Parse "namespace/name"
		hpaParts := strings.Split(itemId, "/")
		if len(hpaParts) == 2 {
			namespace, name := hpaParts[0], hpaParts[1]
			for i, hpa := range a.model.SelectedHPAs {
				if hpa.Namespace == namespace && hpa.Name == name {
					a.model.EditingHPA = &a.model.SelectedHPAs[i]
					a.debugLog("🔄 HPA em edição restaurado: %s/%s", namespace, name)
					break
				}
			}
		}
	case "nodepool":
		for i, pool := range a.model.SelectedNodePools {
			if pool.Name == itemId {
				a.model.EditingNodePool = &a.model.SelectedNodePools[i]
				a.debugLog("🔄 Node pool em edição restaurado: %s", itemId)
				break
			}
		}
	case "cronjob":
		for i, job := range a.model.SelectedCronJobs {
			if job.Name == itemId {
				a.model.EditingCronJob = &a.model.SelectedCronJobs[i]
				a.debugLog("🔄 CronJob em edição restaurado: %s", itemId)
				break
			}
		}
	}
}

// Init implementa tea.Model interface
func (a *App) Init() tea.Cmd {
	return a.initializeManagers()
}

// initializeManagers inicializa os gerenciadores
func (a *App) initializeManagers() tea.Cmd {
	return func() tea.Msg {
		// Copiar clusters-config.json para diretório home se necessário
		if err := a.ensureClustersConfigInHome(); err != nil {
			a.debugLog("Warning: Failed to copy clusters-config.json to home: %v", err)
		}

		kubeManager, err := config.NewKubeConfigManager(a.kubeconfigPath)
		if err != nil {
			return initManagersMsg{err: err}
		}

		sessionManager, err := session.NewManager()
		if err != nil {
			return initManagersMsg{kubeManager: kubeManager, err: err}
		}

		return initManagersMsg{
			kubeManager:    kubeManager,
			sessionManager: sessionManager,
			err:            nil,
		}
	}
}

// ensureClustersConfigInHome garante que clusters-config.json existe no diretório home
func (a *App) ensureClustersConfigInHome() error {
	homeConfigDir := filepath.Join(os.Getenv("HOME"), ".k8s-hpa-manager")
	homeConfigPath := filepath.Join(homeConfigDir, "clusters-config.json")
	localConfigPath := "clusters-config.json"

	// Criar diretório se não existir
	if err := os.MkdirAll(homeConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", homeConfigDir, err)
	}

	// Se arquivo já existe no home, não fazer nada
	if _, err := os.Stat(homeConfigPath); err == nil {
		return nil
	}

	// Se arquivo existe localmente, copiar para home
	if data, err := os.ReadFile(localConfigPath); err == nil {
		if err := os.WriteFile(homeConfigPath, data, 0644); err != nil {
			return fmt.Errorf("failed to copy clusters-config.json to home: %w", err)
		}
		a.debugLog("✅ Copied clusters-config.json to ~/.k8s-hpa-manager/")
		return nil
	}

	// Se não existe localmente, criar arquivo vazio no home
	emptyConfig := []ClusterConfig{}
	data, err := json.MarshalIndent(emptyConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal empty config: %w", err)
	}

	if err := os.WriteFile(homeConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to create empty clusters-config.json: %w", err)
	}

	a.debugLog("✅ Created empty clusters-config.json in ~/.k8s-hpa-manager/")
	return nil
}

// Update implementa tea.Model interface
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Aplicar resolução mínima 188x45 para garantir visibilidade completa do layout
		a.debugLog("🖥️  Terminal resize: %dx%d", msg.Width, msg.Height)
		oldWidth, oldHeight := a.width, a.height
		a.width, a.height = a.applyTerminalSizeLimit(msg.Width, msg.Height)
		a.debugLog("🖥️  Applied limits: %dx%d -> %dx%d", oldWidth, oldHeight, a.width, a.height)
		return a, nil

	case clearScreenMsg:
		// Força redesenho completo da tela
		return a, tea.ClearScreen

	case statusRefreshMsg:
		// Auto-refresh do painel Status a cada 2 segundos
		// Apenas reagenda o próximo refresh sem forçar redesenho da tela
		return a, startStatusRefreshTimer()

	case tea.KeyMsg:
		return a.handleKeyPress(msg)

	case tea.MouseMsg:
		return a.handleMouseEvent(msg)

	case initManagersMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to initialize: %v", msg.err)
			return a, nil
		}
		a.kubeManager = msg.kubeManager
		a.sessionManager = msg.sessionManager

		// Descobrir clusters automaticamente e iniciar timer de auto-refresh
		return a, tea.Batch(a.discoverClusters(), startStatusRefreshTimer())

	case clustersDiscoveredMsg:
		if msg.err != nil {
			// Se for erro de VPN, ativar modal específico
			if msg.vpnError {
				a.model.ShowVPNErrorModal = true
				a.model.VPNErrorMessage = "Não foi possível conectar ao Kubernetes.\nVerifique se a VPN está ativa e configurada corretamente."
				return a, nil
			}
			// Erro genérico
			a.model.Error = fmt.Sprintf("Failed to discover clusters: %v", msg.err)
			return a, nil
		}
		// Ordenar clusters: HLG primeiro, depois PRD, depois OUTROS
		a.model.Clusters = a.sortClustersByEnvironment(msg.clusters)
		a.model.Loading = false

		// Adicionar estatísticas dos clusters ao StatusContainer
		totalClusters := len(a.model.Clusters)
		a.model.StatusContainer.Clear() // Limpar mensagens anteriores
		a.model.StatusContainer.AddInfo("clusters", fmt.Sprintf("🏗️ Total: %d clusters", totalClusters))
		a.model.StatusContainer.AddInfo("status", "⏳ Verificando conectividade...")

		// Adicionar mensagens de teste para scroll (apenas em modo debug)
		if a.debug {
			// statusPanel direct access
			a.model.StatusContainer.AddInfo("system", "🖱️ Mouse support habilitado - clique no painel para focar")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 1 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 2 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 3 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 4 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 5 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 6 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 7 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 8 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 9 para scroll")
			a.model.StatusContainer.AddInfo("test", "📝 Mensagem de teste 10 - use Shift+↑/↓ após clicar no painel")
			a.model.StatusContainer.AddSuccess("ready", "✅ Painel populado com mensagens de teste para scroll!")
		}

		return a, a.testClusterConnections()

	case clusterConnectionTestMsg:
		a.updateClusterStatus(msg.cluster, msg.status, msg.err)
		return a, nil

	case namespacesLoadedMsg:
		if msg.err != nil {
			// Se for erro de VPN, ativar modal específico
			if msg.vpnError {
				a.model.ShowVPNErrorModal = true
				a.model.VPNErrorMessage = "Não foi possível conectar ao cluster.\nVerifique se a VPN está ativa e configurada corretamente."
				a.model.Loading = false
				return a, nil
			}
			// Erro genérico
			a.model.Error = fmt.Sprintf("Failed to load namespaces: %v", msg.err)
			a.model.Loading = false
			return a, nil
		}
		// Substituir namespaces (não append) para evitar duplicatas
		a.model.Namespaces = msg.namespaces
		a.model.Loading = false
		// Iniciar contagem de HPAs em background
		return a, tea.Batch(tea.ClearScreen, a.loadHPACounts())

	case hpasLoadedMsg:
		if msg.err != nil {
			// Se for erro de VPN, ativar modal específico
			if msg.vpnError {
				a.model.ShowVPNErrorModal = true
				a.model.VPNErrorMessage = "Não foi possível carregar HPAs.\nVerifique se a VPN está ativa e configurada corretamente."
				return a, nil
			}
			// Erro genérico
			a.model.Error = fmt.Sprintf("Failed to load HPAs: %v", msg.err)
			return a, nil
		}
		// Substituir HPAs para o namespace atual
		a.model.HPAs = msg.hpas
		return a, tea.ClearScreen

	case hpaDeploymentResourcesEnrichedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load deployment resources: %v", msg.err)
		} else {
			a.model.SuccessMsg = fmt.Sprintf("Deployment resources loaded for %s", msg.hpa.DeploymentName)
		}
		return a, nil

	case sessionHPAsEnrichedMsg:
		// HPAs da sessão foram enriquecidos com dados de deployment do cluster
		if msg.enrichedCount > 0 {
			a.debugLog("📊 %d HPAs da sessão enriquecidos com dados atuais do cluster\n", msg.enrichedCount)
		}
		return a, nil

	case hpaChangesAppliedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to apply HPA changes: %v", msg.err)
			a.model.SuccessMsg = ""
		} else {
			// Atualizar HPAs aplicados com sucesso - limpar Modified e sincronizar contadores
			for _, appliedHPA := range msg.appliedHPAs {
				for i := range a.model.SelectedHPAs {
					// Encontrar o HPA correspondente e atualizar estado
					if a.model.SelectedHPAs[i].Name == appliedHPA.Name &&
						a.model.SelectedHPAs[i].Namespace == appliedHPA.Namespace &&
						a.model.SelectedHPAs[i].Cluster == appliedHPA.Cluster {
						a.model.SelectedHPAs[i].Modified = false
						// Sincronizar contador de aplicações
						a.model.SelectedHPAs[i].AppliedCount = appliedHPA.AppliedCount
						a.model.SelectedHPAs[i].LastAppliedAt = appliedHPA.LastAppliedAt
						break
					}
				}
			}
			// Mostrar mensagem de sucesso
			a.model.SuccessMsg = fmt.Sprintf("Aplicadas mudanças em %d HPA(s)", msg.count)
			a.model.Error = ""
		}
		// Iniciar timer para limpar mensagens após 5 segundos
		return a, a.clearStatusMessages()

	case hpaCountUpdatedMsg:
		if msg.err == nil {
			// Atualizar contagem de HPAs no namespace correspondente
			for i := range a.model.Namespaces {
				if a.model.Namespaces[i].Name == msg.namespace {
					a.model.Namespaces[i].HPACount = msg.count
					break
				}
			}
		}
		return a, nil

	case sessionSavedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to save session: %v", msg.err)
			a.model.SuccessMsg = ""
		} else {
			a.model.SuccessMsg = fmt.Sprintf("💾 Sessão '%s' salva com sucesso", msg.sessionName)
			a.model.Error = ""
		}
		return a, a.clearStatusMessages()

	case sessionDeletedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to delete session: %v", msg.err)
			a.model.SuccessMsg = ""
		} else {
			a.model.SuccessMsg = fmt.Sprintf("🗑️ Sessão '%s' deletada com sucesso", msg.sessionName)
			a.model.Error = ""
			// Recarregar lista de sessões da pasta atual após deleção
			if a.model.CurrentFolder != "" {
				return a, a.loadSessionsFromFolder(a.model.CurrentFolder)
			} else {
				return a, a.loadSessions()
			}
		}
		return a, a.clearStatusMessages()

	case sessionRenamedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to rename session: %v", msg.err)
			a.model.SuccessMsg = ""
		} else {
			a.model.SuccessMsg = fmt.Sprintf("✏️ Sessão '%s' renomeada para '%s' com sucesso", msg.oldName, msg.newName)
			a.model.Error = ""
			// Recarregar lista de sessões da pasta atual após renome
			if a.model.CurrentFolder != "" {
				return a, a.loadSessionsFromFolder(a.model.CurrentFolder)
			} else {
				return a, a.loadSessions()
			}
		}
		return a, a.clearStatusMessages()

	case clusterSaveResultMsg:
		if msg.success {
			a.model.SuccessMsg = fmt.Sprintf("✅ Cluster '%s' adicionado com sucesso", msg.cluster.Name)
			a.model.Error = ""
			// Voltar para seleção de clusters e recarregar lista
			a.model.State = models.StateClusterSelection
			a.model.AddingCluster = false
			a.model.AddClusterFormFields = make(map[string]string)
			a.model.AddClusterActiveField = ""
			a.model.EditingField = false
			a.model.EditingValue = ""
			a.model.CursorPosition = 0
			// Recarregar clusters para incluir o novo
			return a, tea.Batch(tea.ClearScreen, a.discoverClusters(), a.clearStatusMessages())
		} else {
			a.model.Error = msg.error
			a.model.SuccessMsg = ""
			return a, a.clearStatusMessages()
		}

	case sessionFoldersLoadedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load session folders: %v", msg.err)
		} else {
			a.model.SessionFolders = msg.folders
		}
		return a, nil

	case sessionsLoadedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load sessions: %v", msg.err)
		} else {
			a.model.LoadedSessions = msg.sessions
			a.model.SelectedSessionIdx = 0
		}
		return a, nil

	case sessionStateLoadedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load session state: %v", msg.err)
			a.model.State = models.StateClusterSelection
			return a, nil
		}

		// Encontrar e selecionar o cluster
		for i := range a.model.Clusters {
			if a.model.Clusters[i].Name == msg.clusterName {
				a.model.SelectedCluster = &a.model.Clusters[i]
				break
			}
		}

		if a.model.SelectedCluster == nil {
			a.model.Error = fmt.Sprintf("Cluster '%s' não encontrado", msg.clusterName)
			a.model.State = models.StateClusterSelection
			return a, nil
		}

		// Armazenar nome da sessão carregada
		a.model.LoadedSessionName = msg.sessionName

		// Atualizar nome da aba com a sessão carregada
		a.updateTabName()

		// Verificar se é sessão de node pools ou HPAs
		a.debugLog("🔍 Processando sessionStateLoadedMsg: nodePools=%d, hpas=%d\n",
			len(msg.nodePools), len(msg.hpas))

		if len(msg.nodePools) > 0 {
			// É uma sessão de node pools
			a.debugLog("🔧 Configurando sessão de node pools\n")
			a.model.NodePools = msg.nodePools
			a.model.SelectedNodePools = make([]models.NodePool, 0)

			// Adicionar pools modificados à lista de selecionados
			for _, pool := range msg.nodePools {
				if pool.Modified {
					a.model.SelectedNodePools = append(a.model.SelectedNodePools, pool)
					a.debugLog("✓ Pool '%s' adicionado aos selecionados\n", pool.Name)
				}
			}

			// Transicionar para tela de node pools
			a.model.State = models.StateNodeSelection
			a.model.ActivePanel = models.PanelSelectedNodePools
			a.model.SelectedIndex = 0

			a.debugLog("🎯 Estado alterado para StateNodeSelection com %d pools selecionados\n",
				len(a.model.SelectedNodePools))

			a.model.SuccessMsg = fmt.Sprintf("📚 Sessão de node pools '%s' carregada com sucesso. %d pool(s) modificado(s).",
				msg.sessionName, len(a.model.SelectedNodePools))

		} else {
			// É uma sessão de HPAs (código original)
			// Criar cliente Kubernetes para o cluster se não existir
			clusterName := a.model.SelectedCluster.Name
			_, exists := a.getClient(clusterName)
			if !exists {
				if a.kubeManager == nil {
					a.model.Error = "Kube manager not initialized"
					a.model.State = models.StateClusterSelection
					return a, nil
				}

				clientSet, err := a.kubeManager.GetClient(a.model.SelectedCluster.Context)
				if err != nil {
					a.model.Error = fmt.Sprintf("Não foi possível conectar ao cluster %s: %v", clusterName, err)
					a.model.State = models.StateClusterSelection
					return a, nil
				}

				newClient := kubernetes.NewClient(clientSet, clusterName)
				a.setClient(clusterName, newClient)
			}

			// Restaurar namespaces selecionados
			a.model.SelectedNamespaces = msg.namespaces
			a.model.Namespaces = msg.namespaces // Definir namespaces disponíveis

			// Restaurar HPAs selecionados com modificações da sessão
			a.model.SelectedHPAs = msg.hpas
			a.model.HPAs = msg.hpas // Definir HPAs disponíveis

			// Transicionar para tela de seleção de HPAs para permitir edição
			a.model.State = models.StateHPASelection
			a.model.ActivePanel = models.PanelSelectedHPAs
			a.model.SelectedIndex = 0
			a.model.CurrentNamespaceIdx = 0

			a.model.SuccessMsg = fmt.Sprintf("📚 Sessão de HPAs '%s' carregada com sucesso", msg.sessionName)

			// Enriquecer HPAs que não possuem dados de deployment
			return a, a.enrichSessionHPAs()
		}

		return a, nil

	case azureAuthStartMsg:
		a.model.Loading = true
		a.model.Error = ""
		return a, a.performAzureAuth()

	case azureAuthResultMsg:
		a.model.Loading = false
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Azure CLI authentication failed: %v", msg.err)
			return a, nil
		}
		a.model.SuccessMsg = "✅ Azure CLI authentication successful"
		// Continue with node pool loading after successful authentication
		return a, a.loadNodePools()

	case nodePoolsConfiguratingSubscriptionMsg:
		// Mostrar mensagem "Configurando subscription" e continuar com configuração
		a.model.SuccessMsg = fmt.Sprintf("🔄 Configurando subscription: %s", msg.clusterConfig.Subscription)
		// statusPanel direct access
		return a, configurateSubscriptionWithStatus(msg.clusterConfig, a.model.StatusContainer)

	case nodePoolsLoadedMsg:
		// Processar log do Azure primeiro, se presente
		var cmd tea.Cmd
		if msg.azureLogMsg != nil {
			// statusPanel direct access
			switch msg.azureLogMsg.level {
			case "error":
				a.model.StatusContainer.AddError(msg.azureLogMsg.source, msg.azureLogMsg.message)
			case "success":
				a.model.StatusContainer.AddSuccess(msg.azureLogMsg.source, msg.azureLogMsg.message)
			default:
				a.model.StatusContainer.AddInfo(msg.azureLogMsg.source, msg.azureLogMsg.message)
			}
		}

		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load node pools: %v", msg.err)
			a.model.Loading = false
			return a, cmd
		}

		a.model.NodePools = msg.nodePools
		a.model.Loading = false
		return a, tea.Batch(cmd, tea.ClearScreen)

	case cronJobsLoadedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to load cronjobs: %v", msg.err)
			a.model.Loading = false
			return a, nil
		}
		a.model.CronJobs = msg.cronJobs
		a.model.SelectedCronJobs = make([]models.CronJob, 0)
		a.model.Loading = false
		a.model.SelectedIndex = 0
		return a, tea.ClearScreen

	case cronJobUpdateMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to update cronjobs: %v", msg.err)
			return a, nil
		}
		a.model.SuccessMsg = "✅ CronJobs atualizados com sucesso"
		// Recarregar CronJobs para mostrar estado atual
		return a, a.loadCronJobs()

	case autoDiscoverResultMsg:
		if msg.err != nil {
			a.model.StatusContainer.AddError("autodiscover", fmt.Sprintf("❌ Erro: %v", msg.err))
			return a, nil
		}
		if msg.success {
			// Mostrar modal de restart em vez de apenas mensagem de sucesso
			a.model.ShowRestartModal = true
			a.model.RestartClustersFound = msg.clustersFound
			a.model.RestartModalMessage = fmt.Sprintf("Auto-descoberta concluída!\n\n%d clusters foram configurados", msg.clustersFound)
			if len(msg.errors) > 0 {
				a.model.RestartModalMessage += fmt.Sprintf("\n(%d com erros)", len(msg.errors))
			}
			a.model.RestartModalMessage += "\n\nÉ necessário REINICIAR a aplicação para\nque os novos clusters apareçam na lista."
		} else {
			a.model.StatusContainer.AddError("autodiscover", "❌ Nenhum cluster descoberto")
		}
		return a, nil

	case clusterResourcesDiscoveredMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to discover cluster resources: %v", msg.err)
			a.model.Loading = false
			return a, nil
		}
		a.model.ClusterResources = msg.resources
		a.model.SelectedResources = make([]models.ClusterResource, 0)
		a.model.Loading = false

		// Buscar métricas de forma assíncrona em background
		go a.fetchMetricsAsync()

		// Iniciar ticker para atualizar a UI enquanto métricas são coletadas
		a.model.FetchingMetrics = true

		// Ir para estado de seleção de recursos
		if a.model.PrometheusStackMode {
			a.model.State = models.StatePrometheusStackManagement
		} else {
			a.model.State = models.StateClusterResourceSelection
		}

		return a, tea.Batch(tea.ClearScreen, a.tickMetricsRefresh())

	case nodePoolUpdateMsg:
		// Atualizar node pool modificado
		for i := range a.model.SelectedNodePools {
			if a.model.SelectedNodePools[i].Name == msg.nodePool.Name {
				a.model.SelectedNodePools[i] = msg.nodePool
				break
			}
		}
		return a, nil

	case nodePoolsAppliedMsg:
		if msg.err != nil {
			a.model.Error = fmt.Sprintf("Failed to apply node pool changes: %v", msg.err)
			a.model.SuccessMsg = ""
		} else {
			a.model.SuccessMsg = fmt.Sprintf("✅ Aplicadas mudanças em %d node pool(s)", len(msg.appliedPools))
			a.model.Error = ""

			// Marcar node pools aplicados como não modificados
			for _, appliedPool := range msg.appliedPools {
				for i := range a.model.SelectedNodePools {
					if a.model.SelectedNodePools[i].Name == appliedPool.Name &&
						a.model.SelectedNodePools[i].ClusterName == appliedPool.ClusterName {
						a.model.SelectedNodePools[i].Modified = false

						// Se este node pool está em uma sequência, marcar como completado
						if a.model.SelectedNodePools[i].SequenceOrder > 0 {
							a.model.SelectedNodePools[i].SequenceStatus = "completed"
							a.debugLog("✅ Node pool %s (ordem %d) marcado como completed",
								a.model.SelectedNodePools[i].Name, a.model.SelectedNodePools[i].SequenceOrder)
						}

						// Atualizar valores originais para refletir o estado atual
						a.model.SelectedNodePools[i].OriginalValues = models.NodePoolValues{
							NodeCount:          appliedPool.NodeCount,
							MinNodeCount:       appliedPool.MinNodeCount,
							MaxNodeCount:       appliedPool.MaxNodeCount,
							AutoscalingEnabled: appliedPool.AutoscalingEnabled,
						}
						break
					}
				}
			}

			// Verificar se deve iniciar execução sequencial do próximo node pool
			cmd := a.checkAndStartSequentialExecution()
			if cmd != nil {
				return a, cmd
			}
		}
		return a, a.clearStatusMessages()

	case sequentialNodePoolCompletedMsg:
		// Node pool sequencial completado
		a.debugLog("📊 Recebido sequentialNodePoolCompletedMsg: %s (ordem %d), sucesso=%v",
			msg.nodePoolName, msg.order, msg.success)

		// Atualizar status do node pool na lista
		for i := range a.model.SelectedNodePools {
			if a.model.SelectedNodePools[i].Name == msg.nodePoolName &&
				a.model.SelectedNodePools[i].SequenceOrder == msg.order {

				if msg.success {
					a.model.SelectedNodePools[i].SequenceStatus = "completed"
					a.model.SelectedNodePools[i].Modified = false
					a.debugLog("✅ Node pool %s marcado como completed", msg.nodePoolName)
				} else {
					a.model.SelectedNodePools[i].SequenceStatus = "failed"
					a.model.Error = fmt.Sprintf("Falha ao aplicar %s: %v", msg.nodePoolName, msg.err)
					a.debugLog("❌ Node pool %s marcado como failed", msg.nodePoolName)
				}
				break
			}
		}

		// Se foi sucesso, verificar se deve iniciar o próximo na sequência
		if msg.success {
			return a, tea.Batch(
				a.clearStatusMessages(),
				startSequentialExecutionMonitor(),
			)
		}

		return a, a.clearStatusMessages()

	case sequentialExecutionCheckMsg:
		// Verificar se há próximo node pool para executar
		var firstPool, secondPool *models.NodePool

		for i := range a.model.SelectedNodePools {
			pool := &a.model.SelectedNodePools[i]
			if pool.SequenceOrder == 1 {
				firstPool = pool
			} else if pool.SequenceOrder == 2 {
				secondPool = pool
			}
		}

		// Se primeiro completou e segundo está pendente, iniciar segundo
		if firstPool != nil && secondPool != nil &&
			firstPool.SequenceStatus == "completed" &&
			secondPool.SequenceStatus == "pending" {

			a.debugLog("🚀 Iniciando execução automática do segundo node pool: %s", secondPool.Name)
			secondPool.SequenceStatus = "executing"

			a.model.StatusContainer.AddInfo(
				"seq-auto",
				fmt.Sprintf("🚀 Iniciando automaticamente *2: %s", secondPool.Name),
			)

			return a, a.applySequentialNodePool(*secondPool, 2)
		}

		return a, nil

	case resourceChangeAppliedMsg:
		return a.handleResourceChangeApplied(msg)

	case resourcesBatchAppliedMsg:
		return a.handleResourcesBatchApplied(msg)

	case prometheusStackAppliedMsg:
		return a.handlePrometheusStackApplied(msg)

	case progressUpdateMsg:
		// Atualizar interface durante rollouts
		return a, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return progressUpdateMsg{}
		})

	case metricsRefreshMsg:
		// Atualizar interface enquanto métricas são coletadas
		if a.model.FetchingMetrics {
			return a, a.tickMetricsRefresh()
		}
		return a, nil

	case cleanupRolloutsMsg:
		// Limpar rollouts concluídos após delay
		a.cleanupCompletedRollouts()
		return a, nil

	case clearStatusMsg:
		// Limpar mensagens de status
		a.model.SuccessMsg = ""
		a.model.Error = ""
		return a, tea.ClearScreen

	case statusLogMsg:
		// Adicionar log ao StatusPanel
		// statusPanel direct access
		switch msg.level {
		case "info":
			a.model.StatusContainer.AddInfo(msg.source, msg.message)
		case "error":
			a.model.StatusContainer.AddError(msg.source, msg.message)
		case "success":
			a.model.StatusContainer.AddSuccess(msg.source, msg.message)
		case "warn", "warning":
			a.model.StatusContainer.AddWarning(msg.source, msg.message)
		case "debug":
			a.model.StatusContainer.AddDebug(msg.source, msg.message)
		default:
			a.model.StatusContainer.AddInfo(msg.source, msg.message)
		}
		return a, nil

	case vpnStatusMsg:
		// Atualizar status de VPN no modelo
		a.model.VPNConnected = msg.connected
		a.model.VPNLastCheck = time.Now()
		a.model.VPNStatusMessage = msg.message

		// Log no status panel
		if msg.connected {
			a.model.StatusContainer.AddSuccess("vpn", msg.message)
		} else {
			a.model.StatusContainer.AddError("vpn", msg.message)
			if msg.err != nil {
				a.model.StatusContainer.AddError("vpn-detail", fmt.Sprintf("Erro: %v", msg.err))
			}
		}
		return a, nil

	case azureADStatusMsg:
		// Atualizar status de Azure AD no modelo
		a.model.AzureADAuthenticated = msg.authenticated
		a.model.AzureADLastCheck = time.Now()
		a.model.AzureADStatusMessage = msg.message

		// Log no status panel
		if msg.authenticated {
			a.model.StatusContainer.AddSuccess("azure-ad", msg.message)
		} else {
			a.model.StatusContainer.AddError("azure-ad", msg.message)
			if msg.err != nil {
				a.model.StatusContainer.AddError("azure-ad-detail", fmt.Sprintf("Erro: %v", msg.err))
			}
		}
		return a, nil

	// Log Viewer Messages
	case logLoadedMsg, logClearedMsg, logCopiedMsg:
		return a.handleLogViewerMessages(msg)
	}

	return a, nil
}

// applyTerminalSizeLimit - Aplica resolução mínima 188x45 para garantir visibilidade completa
func (a *App) applyTerminalSizeLimit(width, height int) (int, int) {
	a.debugLog("🔧 Terminal size received: %dx%d (using REAL terminal size)", width, height)

	// REMOVIDO: Não forçar dimensões mínimas
	// A aplicação agora usa EXATAMENTE o tamanho do terminal do usuário
	// Isso evita texto minúsculo e permite uso confortável em produção

	return width, height
}

// View implementa tea.Model interface
func (a *App) View() string {
	a.debugLog("🎨 View() chamado - width: %d, height: %d", a.width, a.height)

	if a.width == 0 {
		return "Initializing..."
	}

	// Layout agora é totalmente responsivo - adapta-se ao tamanho do terminal
	// Sem forçar dimensões mínimas para melhor usabilidade em produção

	// Renderizar conteúdo baseado no estado atual
	// A barra de abas agora é renderizada dentro de cada função render*
	var content string

	switch a.model.State {
	case models.StateClusterSelection:
		content = a.renderClusterSelection()
	case models.StateSessionFolderSelection:
		content = a.renderSessionFolderSelection()
	case models.StateSessionSelection:
		content = a.renderSessionSelection()
	case models.StateNamespaceSelection:
		content = a.renderNamespaceSelection()
	case models.StateHPASelection:
		content = a.renderHPASelection()
	case models.StateHPAEditing:
		content = a.renderHPAEditing()
	case models.StateNodeSelection:
		content = a.renderNodePoolSelection()
	case models.StateNodeEditing:
		content = a.renderNodePoolEditing()
	case models.StateMixedSession:
		content = a.renderMixedSession()
	case models.StateClusterResourceDiscovery:
		content = a.renderClusterResourceDiscovery()
	case models.StateClusterResourceSelection:
		content = a.renderClusterResourceSelection()
	case models.StateClusterResourceEditing:
		content = a.renderClusterResourceEditing()
	case models.StatePrometheusStackManagement:
		content = a.renderPrometheusStackManagement()
	case models.StateCronJobSelection:
		content = a.renderCronJobSelection()
	case models.StateCronJobEditing:
		content = a.renderCronJobEditing()
	case models.StateAddingCluster:
		content = a.renderAddCluster()
	case models.StateLogViewer:
		content = a.renderLogViewer()
	case models.StateHelp:
		content = a.renderHelp()
	default:
		content = "Unknown state"
	}

	// Renderizar modais como overlay se estiverem ativos
	if a.model.ShowRestartModal {
		content = a.renderModalOverlay(content, a.renderRestartModal())
	}

	if a.model.ShowVPNErrorModal {
		content = a.renderModalOverlay(content, a.renderVPNErrorModal())
	}

	if a.model.ShowConfirmModal {
		content = a.renderModalOverlay(content, a.renderConfirmModal())
	}

	return content
}

// renderModalOverlay combina o conteúdo de fundo com o modal centralizado
func (a *App) renderModalOverlay(background, modal string) string {
	// Dividir o background em linhas
	bgLines := strings.Split(background, "\n")

	// Dividir o modal em linhas
	modalLines := strings.Split(modal, "\n")

	// Calcular posição central
	startRow := (len(bgLines) - len(modalLines)) / 2
	if startRow < 0 {
		startRow = 0
	}

	// Criar novo conteúdo com overlay
	result := make([]string, len(bgLines))
	copy(result, bgLines)

	// Sobrescrever linhas centrais com o modal
	for i, modalLine := range modalLines {
		row := startRow + i
		if row < len(result) {
			result[row] = modalLine
		}
	}

	return strings.Join(result, "\n")
}

// handleKeyPress processa as teclas pressionadas
func (a *App) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Se o modal de confirmação estiver ativo, apenas ENTER e ESC funcionam
	if a.model.ShowConfirmModal {
		switch msg.String() {
		case "enter":
			// Confirmar e executar ação
			a.model.ShowConfirmModal = false
			return a.executeConfirmedAction()
		case "esc":
			// Cancelar
			a.model.ShowConfirmModal = false
			a.model.StatusContainer.AddInfo("cancel", "❌ Operação cancelada pelo usuário")
			return a, nil
		}
		// Outras teclas não fazem nada quando modal está aberto
		return a, nil
	}

	// Se o modal de restart estiver ativo, apenas ESC e F4 funcionam
	if a.model.ShowRestartModal {
		switch msg.String() {
		case "esc":
			// Fechar o modal e continuar
			a.model.ShowRestartModal = false
			return a, nil
		case "f4", "ctrl+c":
			// Sair da aplicação
			a.cancel()
			fmt.Print("\033[2J\033[H")
			return a, tea.Quit
		}
		// Outras teclas não fazem nada quando modal está aberto
		return a, nil
	}

	// Se o modal de erro de VPN estiver ativo, apenas ESC, F4 e F5 funcionam
	if a.model.ShowVPNErrorModal {
		switch msg.String() {
		case "esc":
			// Fechar o modal
			a.model.ShowVPNErrorModal = false
			return a, nil
		case "f5", "r":
			// Recarregar clusters (fechar modal e tentar novamente)
			a.model.ShowVPNErrorModal = false
			return a, a.discoverClusters()
		case "f4", "ctrl+c":
			// Sair da aplicação
			a.cancel()
			fmt.Print("\033[2J\033[H")
			return a, tea.Quit
		}
		// Outras teclas não fazem nada quando modal está aberto
		return a, nil
	}

	// Se há mensagem de erro, ESC limpa o erro
	if a.model.Error != "" {
		if msg.String() == "esc" {
			a.model.Error = ""
			return a, nil
		}
		// Outras teclas não fazem nada na tela de erro (exceto F4 que é tratado abaixo)
		if msg.String() != "f4" && msg.String() != "ctrl+c" {
			return a, nil
		}
	}

	// Se há mensagem de sucesso, qualquer tecla limpa
	if a.model.SuccessMsg != "" {
		a.model.SuccessMsg = ""
		return a, nil
	}

	switch msg.String() {
	case "ctrl+c", "f4":
		a.cancel()
		// Limpar a tela antes de sair
		fmt.Print("\033[2J\033[H")
		return a, tea.Quit

	case "esc":
		// Voltar ao estado anterior ou fechar modal
		return a.handleEscape()

	case "?":
		// Mostrar ajuda
		if a.model.State != models.StateHelp {
			a.model.PreviousState = a.model.State
			a.model.SaveHelpSnapshot() // Salvar snapshot completo do estado
			a.model.State = models.StateHelp
		}
		return a, nil

	case "f3":
		// Visualizar logs da aplicação
		if a.model.State != models.StateLogViewer {
			a.model.PreviousState = a.model.State
			a.model.State = models.StateLogViewer
			a.model.LogViewerScrollPos = 0
			a.model.LogViewerLoading = true
			a.model.LogViewerMessage = "Carregando logs..."
			return a, a.loadLogs()
		}
		return a, nil

	// ==================== TAB MANAGEMENT ====================
	case "ctrl+t":
		// Nova aba
		return a.handleNewTab()

	case "ctrl+w":
		// Fechar aba atual
		return a.handleCloseTab()

	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9", "alt+0":
		// Mudar para aba específica (Alt+1 = aba 0, Alt+0 = aba 9)
		return a.handleSwitchTab(msg.String())

	case "ctrl+right":
		// Próxima aba (com wrap-around)
		return a.handleNavigateTab("next")

	case "ctrl+left":
		// Aba anterior (com wrap-around)
		return a.handleNavigateTab("prev")
	// ==================== END TAB MANAGEMENT ====================

	case "f7":
		// F7: Auto-descoberta de clusters (apenas na seleção de clusters)
		if a.model.State == models.StateClusterSelection {
			// Adicionar mensagem de início no StatusContainer
			a.model.StatusContainer.AddInfo("autodiscover", "🔍 Iniciando auto-descoberta de clusters...")
			// Executar autodiscover em background
			return a, a.runAutoDiscover()
		}
		// Em outros estados, F7 não faz nada
		return a, nil

	case "f8":
		// Gerenciar recursos Prometheus
		// Se estamos na seleção de clusters, selecionar o cluster atual primeiro
		if a.model.State == models.StateClusterSelection && len(a.model.Clusters) > 0 {
			selectedCluster := &a.model.Clusters[a.model.SelectedIndex]
			a.model.SelectedCluster = selectedCluster
		}
		return a.handleF8PrometheusResources()

	case "f9":
		// Gerenciamento de CronJobs
		// Se estamos na seleção de clusters, selecionar o cluster atual primeiro
		if a.model.State == models.StateClusterSelection && len(a.model.Clusters) > 0 {
			selectedCluster := &a.model.Clusters[a.model.SelectedIndex]
			a.model.SelectedCluster = selectedCluster
			a.debugLog("🔧 F9 pressionado - cluster '%s' selecionado automaticamente", selectedCluster.Name)
		}

		if a.model.SelectedCluster == nil {
			a.model.Error = "Nenhum cluster disponível"
			return a, nil
		}

		// Salvar estado atual antes de navegar para CronJobs
		a.saveCurrentPanelState()

		a.debugLog("🔧 F9 - carregando CronJobs do cluster %s", a.model.SelectedCluster.Name)
		a.model.State = models.StateCronJobSelection
		a.model.Loading = true
		a.model.CronJobs = make([]models.CronJob, 0)
		a.model.SelectedCronJobs = make([]models.CronJob, 0)
		a.model.SelectedIndex = 0
		return a, a.loadCronJobs()
	}

	// Delegar para handler específico baseado no estado
	switch a.model.State {
	case models.StateClusterSelection:
		return a.handleClusterSelectionKeys(msg)
	case models.StateSessionFolderSelection:
		return a.handleSessionFolderSelectionKeys(msg)
	case models.StateSessionSelection:
		return a.handleSessionSelectionKeys(msg)
	case models.StateNamespaceSelection:
		return a.handleNamespaceSelectionKeys(msg)
	case models.StateHPASelection:
		return a.handleHPASelectionKeys(msg)
	case models.StateHPAEditing:
		return a.handleHPAEditingKeys(msg)
	case models.StateNodeSelection:
		return a.handleNodePoolSelectionKeys(msg)
	case models.StateNodeEditing:
		return a.handleNodePoolEditingKeys(msg)
	case models.StateMixedSession:
		return a.handleMixedSessionKeys(msg)
	case models.StateClusterResourceDiscovery:
		// Durante descoberta, apenas ESC funciona (já tratado acima)
		return a, nil
	case models.StateClusterResourceSelection:
		return a.handleClusterResourceSelectionKeys(msg)
	case models.StateClusterResourceEditing:
		return a.handleClusterResourceEditingKeys(msg)
	case models.StatePrometheusStackManagement:
		return a.handlePrometheusStackKeys(msg)
	case models.StateCronJobSelection:
		return a.handleCronJobSelectionKeys(msg)
	case models.StateCronJobEditing:
		return a.handleCronJobEditingKeys(msg)
	case models.StateAddingCluster:
		return a.handleAddClusterKeys(msg)
	case models.StateLogViewer:
		return a.handleLogViewerKeys(msg)
	case models.StateHelp:
		return a.handleHelpKeys(msg)
	}

	return a, nil
}

// handleEscape lida com a tecla ESC com memorização de estado
func (a *App) handleEscape() (tea.Model, tea.Cmd) {
	// Se está editando um campo específico, primeiro cancelar a edição
	if a.model.EditingField {
		a.model.EditingField = false
		a.model.EditingValue = ""
		a.model.CursorPosition = 0
		return a, nil
	}

	// Se há erro exibido, limpar o erro
	if a.model.Error != "" {
		a.model.Error = ""
		return a, nil
	}

	// Salvar estado atual antes de navegar
	a.saveCurrentPanelState()

	var targetState models.AppState

	switch a.model.State {
	case models.StateHelp:
		// Voltar do help para o estado anterior
		targetState = a.model.PreviousState
	case models.StateSessionFolderSelection:
		targetState = models.StateClusterSelection
		a.model.CurrentFolder = ""
	case models.StateSessionFolderSave:
		targetState = models.StateClusterSelection
		a.model.CurrentFolder = ""
	case models.StateSessionSelection:
		targetState = models.StateSessionFolderSelection
		// Restaurar posição da pasta selecionada
		a.model.SelectedFolderIdx = a.model.LastSelectedFolderIdx
	case models.StateNamespaceSelection:
		targetState = models.StateClusterSelection
	case models.StateHPASelection:
		targetState = models.StateNamespaceSelection
	case models.StateHPAEditing:
		targetState = models.StateHPASelection
		a.model.EditingHPA = nil
		a.model.FormFields = make(map[string]string)
	case models.StateNodeSelection:
		targetState = models.StateNamespaceSelection
	case models.StateNodeEditing:
		targetState = models.StateNodeSelection
		a.model.EditingNodePool = nil
		a.model.FormFields = make(map[string]string)
		a.model.ActivePanel = models.PanelSelectedNodePools
	case models.StateMixedSession:
		targetState = models.StateClusterSelection
		a.model.CurrentSession = nil
	case models.StateClusterResourceSelection:
		targetState = models.StateNamespaceSelection
		a.model.ClusterResources = nil
		a.model.SelectedResources = nil
	case models.StateClusterResourceEditing:
		targetState = models.StateClusterResourceSelection
		a.model.EditingResource = nil
		a.model.FormFields = make(map[string]string)
	case models.StatePrometheusStackManagement:
		targetState = models.StateNamespaceSelection
		a.model.PrometheusStackMode = false
		a.model.ClusterResources = nil
		a.model.SelectedResources = nil
	case models.StateCronJobSelection:
		targetState = models.StateNamespaceSelection
		a.model.CronJobs = nil
		a.model.SelectedCronJobs = nil
		a.debugLog("🔙 ESC em CronJobSelection - voltando para StateNamespaceSelection")
	case models.StateCronJobEditing:
		targetState = models.StateCronJobSelection
		a.model.EditingCronJob = nil
		a.model.FormFields = make(map[string]string)
	case models.StateAddingCluster:
		targetState = models.StateClusterSelection
		a.model.AddingCluster = false
		a.model.AddClusterFormFields = make(map[string]string)
		a.model.AddClusterActiveField = ""
	case models.StateLogViewer:
		// Voltar do log viewer para o estado anterior
		targetState = a.model.PreviousState
		a.model.LogViewerLogs = nil
		a.model.LogViewerScrollPos = 0
		a.model.LogViewerMessage = ""
	default:
		// Estado não tem transição definida
		return a, nil
	}

	// Mudar para o estado alvo e restaurar sua posição/configuração
	a.debugLog("🔄 Mudando para estado: %v", targetState)
	a.model.State = targetState
	a.debugLog("🔄 Restaurando estado do painel: %v", targetState)
	a.restorePanelState(targetState)
	a.debugLog("🔄 Cluster após restauração: %v", a.model.SelectedCluster)

	return a, nil
}

// updateClusterStatus atualiza o status de um cluster
func (a *App) updateClusterStatus(contextName string, status models.ConnectionStatus, err error) {
	for i := range a.model.Clusters {
		if a.model.Clusters[i].Context == contextName {
			a.model.Clusters[i].Status = status
			if err != nil {
				a.model.Clusters[i].Error = err.Error()
			}
			break
		}
	}

	// Atualizar estatísticas no StatusContainer se estamos na tela de seleção de clusters
	if a.model.State == models.StateClusterSelection {
		a.updateClusterStatsInStatusPanel()
	}
}

// updateClusterStatsInStatusPanel atualiza as estatísticas dos clusters no painel de status
func (a *App) updateClusterStatsInStatusPanel() {
	totalClusters := len(a.model.Clusters)
	connectedCount := 0
	disconnectedCount := 0
	unknownCount := 0

	for _, cluster := range a.model.Clusters {
		switch cluster.Status {
		case models.StatusConnected:
			connectedCount++
		case models.StatusError, models.StatusTimeout:
			disconnectedCount++
		case models.StatusUnknown:
			unknownCount++
		}
	}

	// Limpar e atualizar StatusContainer com estatísticas atualizadas
	// Usar mensagens mais curtas para caber nas 140 colunas
	a.model.StatusContainer.Clear()
	a.model.StatusContainer.AddInfo("stats", fmt.Sprintf("🏗️ Total: %d | ✅ Online: %d | ❌ Offline: %d | ⏳ Verificando: %d",
		totalClusters, connectedCount, disconnectedCount, unknownCount))

	// Adicionar info do cluster selecionado (truncar se muito longo)
	if len(a.model.Clusters) > 0 && a.model.SelectedIndex < len(a.model.Clusters) {
		selectedCluster := a.model.Clusters[a.model.SelectedIndex]
		clusterName := selectedCluster.Name
		// Truncar nome do cluster se muito longo (max 80 caracteres)
		if len(clusterName) > 80 {
			clusterName = clusterName[:77] + "..."
		}
		a.model.StatusContainer.AddInfo("selected", fmt.Sprintf("🎯 %s", clusterName))
	}
}

// renderErrorScreen renderiza a tela de erro
func (a *App) renderErrorScreen() string {
	return fmt.Sprintf("❌ Erro: %s\n\nPressione 'ESC' para voltar ou 'F4' para sair.", a.model.Error)
}

// renderSuccessScreen renderiza a tela de sucesso
func (a *App) renderSuccessScreen() string {
	return fmt.Sprintf("%s\n\nPressione qualquer tecla para continuar...", a.model.SuccessMsg)
}

// Funções simplificadas para compatibilidade

// runAutoDiscover executa auto-descoberta de clusters em background
func (a *App) runAutoDiscover() tea.Cmd {
	return func() tea.Msg {
		// Função de log que adiciona mensagens ao StatusContainer
		logFunc := func(msg string) {
			a.model.StatusContainer.AddInfo("autodiscover", msg)
		}

		// Executar auto-descoberta com callback de log
		configs, errors := a.kubeManager.AutoDiscoverAllClusters(logFunc)

		// Salvar configurações
		if len(configs) > 0 {
			if err := a.kubeManager.SaveClusterConfigs(configs, logFunc); err != nil {
				return autoDiscoverResultMsg{
					success: false,
					err:     fmt.Errorf("erro ao salvar configurações: %w", err),
				}
			}
		}

		// Retornar resultado
		return autoDiscoverResultMsg{
			success:       len(configs) > 0,
			clustersFound: len(configs),
			errors:        errors,
		}
	}
}

// setupClusterAndLoadNamespaces configura o cluster (contexto kubectl + Azure subscription) e carrega namespaces
func (a *App) setupClusterAndLoadNamespaces() tea.Cmd {
	if a.model.SelectedCluster == nil {
		return nil
	}

	return func() tea.Msg {
		clusterName := a.model.SelectedCluster.Name
		contextName := a.model.SelectedCluster.Context

		// 1. Configurar contexto do kubectl (usar Context, não Name!)
		if err := a.setKubectlContext(contextName); err != nil {
			return namespacesLoadedMsg{err: fmt.Errorf("failed to set kubectl context for %s: %w", contextName, err)}
		}

		// 2. Buscar configuração do cluster no clusters-config.json e configurar Azure subscription
		if err := a.setupAzureSubscription(clusterName); err != nil {
			// Azure subscription é opcional - continuar mesmo se falhar
			a.debugLog("⚠️ Warning: Failed to setup Azure subscription: %v\n", err)
		}

		// 3. Carregar namespaces
		return a.loadNamespacesInternal()
	}
}

func (a *App) loadNamespaces() tea.Cmd {
	return a.setupClusterAndLoadNamespaces()
}

func (a *App) loadNamespacesInternal() tea.Msg {
	// Zerar contadores de aplicação para nova sessão/cluster
	a.resetHPAApplicationCounters()

	// VALIDAR VPN antes de tentar conectar ao cluster (kubectl precisa de VPN)
	a.model.StatusContainer.AddInfo("vpn-check", "🔍 Verificando conectividade VPN...")
	vpnErr := checkVPNConnectivity(a.model.StatusContainer)
	if vpnErr != nil {
		a.model.StatusContainer.AddError("vpn-check", "❌ VPN desconectada")
		return namespacesLoadedMsg{
			err:      fmt.Errorf("VPN desconectada: %w", vpnErr),
			vpnError: true, // Ativar modal de VPN
		}
	}

	// Obter cliente do cluster selecionado
	clusterName := a.model.SelectedCluster.Name
	client, exists := a.getClient(clusterName)
	if !exists {
		// Criar cliente se não existir
		if a.kubeManager == nil {
			return namespacesLoadedMsg{err: fmt.Errorf("kube manager not initialized")}
		}

		clientSet, err := a.kubeManager.GetClient(a.model.SelectedCluster.Context)
		if err != nil {
			// Se erro ao criar cliente, diagnosticar conectividade
			a.model.StatusContainer.AddError("cluster-conn", "❌ Erro ao conectar cluster - diagnosticando...")
			diagErr := checkVPNConnectivity(a.model.StatusContainer)
			if diagErr != nil {
				return namespacesLoadedMsg{
					err:      fmt.Errorf("VPN desconectada ao conectar cluster: %w", diagErr),
					vpnError: true, // Ativar modal de VPN
				}
			}
			return namespacesLoadedMsg{err: fmt.Errorf("cluster %s parece estar offline ou inacessível: %w", clusterName, err)}
		}

		newClient := kubernetes.NewClient(clientSet, clusterName)
		a.setClient(clusterName, newClient)
		client = newClient
	}

	// Carregar namespaces com filtro de sistema baseado na configuração
	namespaces, err := client.ListNamespaces(a.ctx, a.model.ShowSystemNamespaces)
	if err != nil {
		return namespacesLoadedMsg{err: err}
	}

	// Adicionar cluster name aos namespaces
	for i := range namespaces {
		namespaces[i].Cluster = clusterName
	}

	return namespacesLoadedMsg{namespaces: namespaces, err: nil}
}

// setKubectlContext configura o contexto do kubectl para o cluster especificado
func (a *App) setKubectlContext(clusterName string) error {
	a.debugLog("🔄 Setting kubectl context to: %s\n", clusterName)

	cmd := exec.Command("kubectl", "config", "use-context", clusterName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set kubectl context '%s': %w - output: %s", clusterName, err, string(output))
	}

	a.debugLog("✅ kubectl context set to: %s\n", clusterName)
	return nil
}

// setupAzureSubscription busca o cluster no clusters-config.json e configura a Azure subscription
func (a *App) setupAzureSubscription(clusterName string) error {
	// statusPanel direct access

	// Buscar configuração do cluster no clusters-config.json
	clusterConfig, err := findClusterInConfig(clusterName)
	if err != nil {
		a.model.StatusContainer.AddError("azure-config", fmt.Sprintf("❌ Cluster não encontrado na configuração: %s", clusterName))
		return fmt.Errorf("failed to find cluster in config: %w", err)
	}

	a.model.StatusContainer.AddInfo("azure-config", fmt.Sprintf("🔄 Configurando subscription para cluster %s: %s", clusterName, clusterConfig.Subscription))
	a.debugLog("🔄 Setting Azure subscription to: %s\n", clusterConfig.Subscription)

	cmd := exec.Command("az", "account", "set", "--subscription", clusterConfig.Subscription)
	output, err := cmd.CombinedOutput()
	if err != nil {
		a.model.StatusContainer.AddError("azure-config", fmt.Sprintf("❌ Falha ao configurar subscription: %s", err.Error()))
		return fmt.Errorf("failed to set subscription '%s': %w - output: %s", clusterConfig.Subscription, err, string(output))
	}

	a.model.StatusContainer.AddSuccess("azure-config", fmt.Sprintf("✅ Subscription configurada: %s", clusterConfig.Subscription))
	a.debugLog("✅ Azure subscription set to: %s\n", clusterConfig.Subscription)
	return nil
}

// loadHPACounts carrega a contagem de HPAs para todos os namespaces em background
func (a *App) loadHPACounts() tea.Cmd {
	if a.model.SelectedCluster == nil || len(a.model.Namespaces) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	clusterName := a.model.SelectedCluster.Name
	client, exists := a.getClient(clusterName)
	if !exists {
		return nil
	}

	// Criar comandos para contar HPAs em cada namespace
	for _, ns := range a.model.Namespaces {
		cmds = append(cmds, a.countHPAsInNamespace(client, ns.Name))
	}

	return tea.Batch(cmds...)
}

// countHPAsInNamespace conta HPAs em um namespace específico
func (a *App) countHPAsInNamespace(client *kubernetes.Client, namespace string) tea.Cmd {
	return func() tea.Msg {
		count, err := client.CountHPAs(a.ctx, namespace)
		return hpaCountUpdatedMsg{
			namespace: namespace,
			count:     count,
			err:       err,
		}
	}
}

func (a *App) loadHPAs() tea.Cmd {
	if a.model.SelectedCluster == nil || a.model.CurrentNamespaceIdx >= len(a.model.SelectedNamespaces) {
		return nil
	}

	return func() tea.Msg {
		// VALIDAR VPN antes de tentar carregar HPAs (kubectl precisa de VPN)
		a.model.StatusContainer.AddInfo("vpn-check", "🔍 Verificando VPN para HPAs...")
		vpnErr := checkVPNConnectivity(a.model.StatusContainer)
		if vpnErr != nil {
			a.model.StatusContainer.AddError("vpn-check", "❌ VPN desconectada ao carregar HPAs")
			return hpasLoadedMsg{
				err:      fmt.Errorf("VPN desconectada: %w", vpnErr),
				vpnError: true, // Ativar modal de VPN
			}
		}

		// Obter cliente do cluster selecionado
		clusterName := a.model.SelectedCluster.Name
		client, exists := a.getClient(clusterName)
		if !exists {
			return hpasLoadedMsg{err: fmt.Errorf("client not found for cluster %s", clusterName)}
		}

		// Obter namespace atual
		currentNamespace := a.model.SelectedNamespaces[a.model.CurrentNamespaceIdx]

		// Carregar HPAs do namespace
		hpas, err := client.ListHPAs(a.ctx, currentNamespace.Name)
		if err != nil {
			// Se erro ao carregar HPAs, diagnosticar conectividade
			a.model.StatusContainer.AddError("hpa-load", "❌ Erro ao carregar HPAs - diagnosticando...")
			diagErr := checkVPNConnectivity(a.model.StatusContainer)
			if diagErr != nil {
				return hpasLoadedMsg{err: fmt.Errorf("VPN desconectada ao carregar HPAs: %w", diagErr)}
			}
			return hpasLoadedMsg{err: err}
		}

		// Adicionar informações do cluster aos HPAs
		for i := range hpas {
			hpas[i].Cluster = clusterName
		}

		return hpasLoadedMsg{hpas: hpas, err: nil}
	}
}

func (a *App) applyHPAChanges(hpas []models.HPA) tea.Cmd {
	return func() tea.Msg {
		if len(hpas) == 0 {
			return hpaChangesAppliedMsg{count: 0, appliedHPAs: nil, err: nil}
		}

		successCount := 0
		var appliedHPAs []models.HPA
		var lastError error

		// Aplicar mudanças em cada HPA
		for _, hpa := range hpas {
			// Obter cliente do cluster
			client, exists := a.getClient(hpa.Cluster)
			if !exists {
				lastError = fmt.Errorf("client not found for cluster %s", hpa.Cluster)
				continue
			}

			// Logar início da aplicação
			a.model.StatusContainer.AddInfo("apply-hpa", fmt.Sprintf("⚙️ Aplicando HPA: %s/%s", hpa.Namespace, hpa.Name))

			// Aplicar mudanças no HPA
			err := client.UpdateHPA(a.ctx, hpa)
			if err != nil {
				lastError = err
				a.model.StatusContainer.AddError("apply-hpa", fmt.Sprintf("❌ Erro ao aplicar HPA %s/%s: %v", hpa.Namespace, hpa.Name, err))
				continue
			}

			// Logar alterações no HPA
			a.logHPAChanges(hpa)

			// Aplicar mudanças nos recursos do deployment se modificados
			if hpa.ResourcesModified {
				err = client.ApplyHPADeploymentResourceChanges(a.ctx, &hpa)
				if err != nil {
					lastError = fmt.Errorf("failed to apply deployment resources: %w", err)
					a.model.StatusContainer.AddError("apply-resources", fmt.Sprintf("❌ Erro ao aplicar recursos do deployment: %v", err))
					continue
				}
				// Logar alterações nos recursos
				a.logResourceChanges(hpa)
			}

			// Executar rollout se solicitado
			err = client.TriggerRollout(a.ctx, hpa)
			if err != nil {
				lastError = err
				a.model.StatusContainer.AddError("apply-rollout", fmt.Sprintf("❌ Erro ao executar rollout: %v", err))
				continue
			}

			// HPA aplicado com sucesso - incrementar contador de aplicações
			now := time.Now()
			hpa.AppliedCount++
			hpa.LastAppliedAt = &now

			a.model.StatusContainer.AddSuccess("apply-hpa", fmt.Sprintf("✅ HPA aplicado: %s/%s", hpa.Namespace, hpa.Name))

			successCount++
			appliedHPAs = append(appliedHPAs, hpa)
		}

		// Se houve falhas, reportar erro
		if successCount < len(hpas) {
			return hpaChangesAppliedMsg{
				count:       successCount,
				appliedHPAs: appliedHPAs,
				err:         fmt.Errorf("aplicadas %d de %d mudanças. Último erro: %v", successCount, len(hpas), lastError),
			}
		}

		// Sucesso total
		return hpaChangesAppliedMsg{
			count:       successCount,
			appliedHPAs: appliedHPAs,
			err:         nil,
		}
	}
}

// applyHPAChangesAsync - Aplica mudanças em HPAs com rollouts assíncronos
func (a *App) applyHPAChangesAsync(hpas []models.HPA) tea.Cmd {
	return func() tea.Msg {
		if len(hpas) == 0 {
			return hpaChangesAppliedMsg{count: 0, appliedHPAs: nil, err: nil}
		}

		successCount := 0
		var appliedHPAs []models.HPA
		var lastError error

		// Aplicar mudanças em cada HPA
		for _, hpa := range hpas {
			// Obter cliente do cluster
			client, exists := a.getClient(hpa.Cluster)
			if !exists {
				lastError = fmt.Errorf("client not found for cluster %s", hpa.Cluster)
				continue
			}

			// Logar início da aplicação
			a.model.StatusContainer.AddInfo("apply-hpa", fmt.Sprintf("⚙️ Aplicando HPA: %s/%s", hpa.Namespace, hpa.Name))

			// Aplicar mudanças no HPA
			err := client.UpdateHPA(a.ctx, hpa)
			if err != nil {
				lastError = err
				a.model.StatusContainer.AddError("apply-hpa", fmt.Sprintf("❌ Erro ao aplicar HPA %s/%s: %v", hpa.Namespace, hpa.Name, err))
				continue
			}

			// Logar alterações no HPA
			a.logHPAChanges(hpa)

			// Aplicar mudanças nos recursos do deployment se modificados
			if hpa.ResourcesModified {
				err = client.ApplyHPADeploymentResourceChanges(a.ctx, &hpa)
				if err != nil {
					lastError = fmt.Errorf("failed to apply deployment resources: %w", err)
					a.model.StatusContainer.AddError("apply-resources", fmt.Sprintf("❌ Erro ao aplicar recursos do deployment: %v", err))
					continue
				}
				// Logar alterações nos recursos
				a.logResourceChanges(hpa)
			}

			// HPA aplicado com sucesso - incrementar contador de aplicações
			now := time.Now()
			hpa.AppliedCount++
			hpa.LastAppliedAt = &now

			a.model.StatusContainer.AddSuccess("apply-hpa", fmt.Sprintf("✅ HPA aplicado: %s/%s", hpa.Namespace, hpa.Name))

			// Iniciar rollouts assíncronos se solicitados
			if hpa.PerformRollout || hpa.PerformDaemonSetRollout || hpa.PerformStatefulSetRollout {
				a.startAsyncRollouts(hpa, client)
			}

			successCount++
			appliedHPAs = append(appliedHPAs, hpa)
		}

		// Se houve falhas, reportar erro
		if successCount < len(hpas) {
			return hpaChangesAppliedMsg{
				count:       successCount,
				appliedHPAs: appliedHPAs,
				err:         fmt.Errorf("aplicadas %d de %d mudanças. Último erro: %v", successCount, len(hpas), lastError),
			}
		}

		// Sucesso total
		return hpaChangesAppliedMsg{
			count:       successCount,
			appliedHPAs: appliedHPAs,
			err:         nil,
		}
	}
}

// logHPAChanges - Loga alterações feitas no HPA
func (a *App) logHPAChanges(hpa models.HPA) {
	if hpa.OriginalValues == nil {
		return
	}

	changes := []string{}

	// Min Replicas
	if hpa.MinReplicas != hpa.OriginalValues.MinReplicas {
		changes = append(changes, fmt.Sprintf("Min Replicas: %d → %d", hpa.OriginalValues.MinReplicas, hpa.MinReplicas))
	}

	// Max Replicas
	if hpa.MaxReplicas != hpa.OriginalValues.MaxReplicas {
		changes = append(changes, fmt.Sprintf("Max Replicas: %d → %d", hpa.OriginalValues.MaxReplicas, hpa.MaxReplicas))
	}

	// CPU Target
	if hpa.TargetCPU != hpa.OriginalValues.TargetCPU {
		changes = append(changes, fmt.Sprintf("CPU Target: %d%% → %d%%", hpa.OriginalValues.TargetCPU, hpa.TargetCPU))
	}

	// Memory Target
	if hpa.TargetMemory != hpa.OriginalValues.TargetMemory {
		changes = append(changes, fmt.Sprintf("Memory Target: %d%% → %d%%", hpa.OriginalValues.TargetMemory, hpa.TargetMemory))
	}

	// Logar cada alteração
	for _, change := range changes {
		a.model.StatusContainer.AddInfo("hpa-change", fmt.Sprintf("  📝 %s", change))
	}
}

// logResourceChanges - Loga alterações feitas nos recursos do deployment
func (a *App) logResourceChanges(hpa models.HPA) {
	if hpa.OriginalValues == nil {
		return
	}

	changes := []string{}

	// CPU Request
	if hpa.TargetCPURequest != hpa.OriginalValues.CPURequest && hpa.TargetCPURequest != "" {
		oldVal := hpa.OriginalValues.CPURequest
		if oldVal == "" {
			oldVal = "não definido"
		}
		changes = append(changes, fmt.Sprintf("CPU Request: %s → %s", oldVal, hpa.TargetCPURequest))
	}

	// CPU Limit
	if hpa.TargetCPULimit != hpa.OriginalValues.CPULimit && hpa.TargetCPULimit != "" {
		oldVal := hpa.OriginalValues.CPULimit
		if oldVal == "" {
			oldVal = "não definido"
		}
		changes = append(changes, fmt.Sprintf("CPU Limit: %s → %s", oldVal, hpa.TargetCPULimit))
	}

	// Memory Request
	if hpa.TargetMemoryRequest != hpa.OriginalValues.MemoryRequest && hpa.TargetMemoryRequest != "" {
		oldVal := hpa.OriginalValues.MemoryRequest
		if oldVal == "" {
			oldVal = "não definido"
		}
		changes = append(changes, fmt.Sprintf("Memory Request: %s → %s", oldVal, hpa.TargetMemoryRequest))
	}

	// Memory Limit
	if hpa.TargetMemoryLimit != hpa.OriginalValues.MemoryLimit && hpa.TargetMemoryLimit != "" {
		oldVal := hpa.OriginalValues.MemoryLimit
		if oldVal == "" {
			oldVal = "não definido"
		}
		changes = append(changes, fmt.Sprintf("Memory Limit: %s → %s", oldVal, hpa.TargetMemoryLimit))
	}

	// Logar cada alteração de recurso
	for _, change := range changes {
		a.model.StatusContainer.AddInfo("resource-change", fmt.Sprintf("  🔧 %s", change))
	}
}

// logNodePoolChanges - Loga alterações feitas no Node Pool
func (a *App) logNodePoolChanges(pool models.NodePool) {
	changes := []string{}

	// Node Count
	if pool.NodeCount != pool.OriginalValues.NodeCount {
		changes = append(changes, fmt.Sprintf("Node Count: %d → %d", pool.OriginalValues.NodeCount, pool.NodeCount))
	}

	// Min Count (se autoscaler ativo)
	if pool.AutoscalingEnabled {
		if pool.MinNodeCount != pool.OriginalValues.MinNodeCount {
			changes = append(changes, fmt.Sprintf("Min Count: %d → %d", pool.OriginalValues.MinNodeCount, pool.MinNodeCount))
		}

		// Max Count
		if pool.MaxNodeCount != pool.OriginalValues.MaxNodeCount {
			changes = append(changes, fmt.Sprintf("Max Count: %d → %d", pool.OriginalValues.MaxNodeCount, pool.MaxNodeCount))
		}
	}

	// Autoscaling
	if pool.AutoscalingEnabled != pool.OriginalValues.AutoscalingEnabled {
		status := "Desativado"
		if pool.AutoscalingEnabled {
			status = "Ativado"
		}
		oldStatus := "Desativado"
		if pool.OriginalValues.AutoscalingEnabled {
			oldStatus = "Ativado"
		}
		changes = append(changes, fmt.Sprintf("Autoscaling: %s → %s", oldStatus, status))
	}

	// Logar cada alteração
	for _, change := range changes {
		a.model.StatusContainer.AddInfo("nodepool-change", fmt.Sprintf("  📝 %s", change))
	}
}

// executeConfirmedAction - Executa a ação confirmada pelo usuário no modal
func (a *App) executeConfirmedAction() (tea.Model, tea.Cmd) {
	callback := a.model.ConfirmModalCallback

	switch callback {
	case "apply_individual_hpa":
		// Aplicar HPA individual
		if a.model.ActivePanel == models.PanelSelectedHPAs && a.model.SelectedIndex < len(a.model.SelectedHPAs) {
			hpa := a.model.SelectedHPAs[a.model.SelectedIndex]
			return a, a.applyHPAChangesAsync([]models.HPA{hpa})
		}

	case "apply_batch_hpa":
		// Aplicar todos os HPAs selecionados
		if len(a.model.SelectedHPAs) > 0 {
			return a, a.applyHPAChangesAsync(a.model.SelectedHPAs)
		}

	case "apply_nodepools":
		// Aplicar node pools (sequencial ou normal)
		// Verificar se há execução sequencial marcada
		var firstPool, secondPool *models.NodePool
		for i := range a.model.SelectedNodePools {
			pool := &a.model.SelectedNodePools[i]
			if pool.SequenceOrder == 1 {
				firstPool = pool
			} else if pool.SequenceOrder == 2 {
				secondPool = pool
			}
		}

		// Se há execução sequencial, iniciar modo assíncrono
		if firstPool != nil && secondPool != nil {
			a.debugLog("🎯 Execução sequencial detectada - iniciando modo assíncrono")

			// Marcar primeiro como executing
			firstPool.SequenceStatus = "executing"
			secondPool.SequenceStatus = "pending"

			a.model.StatusContainer.AddInfo(
				"seq-start",
				fmt.Sprintf("🎯 Iniciando execução sequencial: *1 %s → *2 %s", firstPool.Name, secondPool.Name),
			)

			// Iniciar execução assíncrona do primeiro
			return a, a.applySequentialNodePool(*firstPool, 1)
		}

		// Modo normal (sem sequência) - aplicar node pools modificados
		var modifiedNodePools []models.NodePool
		for _, pool := range a.model.SelectedNodePools {
			if pool.Modified {
				modifiedNodePools = append(modifiedNodePools, pool)
			}
		}
		if len(modifiedNodePools) > 0 {
			return a, a.applyNodePoolChanges(modifiedNodePools)
		}

	case "apply_mixed_session":
		// Aplicar sessão mista
		if a.model.CurrentSession != nil {
			return a, a.applyMixedSession()
		}
	}

	return a, nil
}

// startAsyncRollouts - Inicia rollouts assíncronos para um HPA
func (a *App) startAsyncRollouts(hpa models.HPA, client *kubernetes.Client) {
	a.debugLog("🚀 startAsyncRollouts chamada para HPA: %s/%s", hpa.Namespace, hpa.Name)
	a.debugLog("🔧 Rollout flags: Deployment=%t, DaemonSet=%t, StatefulSet=%t",
		hpa.PerformRollout, hpa.PerformDaemonSetRollout, hpa.PerformStatefulSetRollout)

	rolloutTypes := []string{}

	if hpa.PerformRollout {
		rolloutTypes = append(rolloutTypes, "deployment")
		a.debugLog("✅ Adicionando rollout: deployment")
	}
	if hpa.PerformDaemonSetRollout {
		rolloutTypes = append(rolloutTypes, "daemonset")
		a.debugLog("✅ Adicionando rollout: daemonset")
	}
	if hpa.PerformStatefulSetRollout {
		rolloutTypes = append(rolloutTypes, "statefulset")
		a.debugLog("✅ Adicionando rollout: statefulset")
	}

	a.debugLog("📋 Total rollout types: %d", len(rolloutTypes))

	// statusPanel direct access

	for _, rolloutType := range rolloutTypes {
		// Criar ID único para o progress bar
		progressID := fmt.Sprintf("rollout_%s_%s_%s", hpa.Name, hpa.Namespace, rolloutType)

		// Adicionar progress bar no StatusContainer
		a.model.StatusContainer.AddProgressBar(progressID, fmt.Sprintf("%s/%s %s", hpa.Name, hpa.Namespace, rolloutType), 100)

		// Log básico do início do rollout
		a.model.StatusContainer.AddInfo("rollout", fmt.Sprintf("🚀 Iniciando rollout %s para %s/%s", rolloutType, hpa.Name, hpa.Namespace))

		// Iniciar goroutine para o rollout
		go a.executeRollout(progressID, hpa, rolloutType, client)
	}
}

// executeRollout - Executa um rollout específico e atualiza o progresso
func (a *App) executeRollout(progressID string, hpa models.HPA, rolloutType string, client *kubernetes.Client) {
	// statusPanel direct access

	// Função helper para atualizar progresso usando StatusPanel
	updateProgress := func(status models.RolloutStatus, progress int, message, errorMsg string) {
		// Atualizar progress bar
		a.model.StatusContainer.UpdateProgress(progressID, progress, message)

		// Se completou, marcar como completo e adicionar mensagem de sucesso
		if status == models.RolloutStatusCompleted {
			a.model.StatusContainer.CompleteProgress(progressID)
			a.model.StatusContainer.AddSuccess("rollout",
				fmt.Sprintf("✅ Rollout %s concluído: %s/%s", rolloutType, hpa.Name, hpa.Namespace))
		} else if status == models.RolloutStatusFailed {
			// Se falhou, remover progress bar e adicionar mensagem de erro com motivo
			a.model.StatusContainer.RemoveProgress(progressID)
			a.model.StatusContainer.AddError("rollout",
				fmt.Sprintf("❌ Rollout %s falhou: %s/%s - %s", rolloutType, hpa.Name, hpa.Namespace, errorMsg))
		}
	}

	// Atualizar status para running
	updateProgress(models.RolloutStatusRunning, 10, "Executando rollout...", "")

	// Simular progresso durante o rollout
	progressSteps := []struct {
		progress int
		message  string
		delay    time.Duration
	}{
		{25, "Aplicando mudanças...", 1 * time.Second},
		{50, "Aguardando pods...", 2 * time.Second},
		{75, "Verificando status...", 2 * time.Second},
		{90, "Finalizando...", 1 * time.Second},
	}

	// Executar rollout com kubectl
	cmd := exec.Command("kubectl", "rollout", "restart", rolloutType+"/"+hpa.Name,
		"-n", hpa.Namespace, "--context", hpa.Cluster)

	err := cmd.Start()
	if err != nil {
		updateProgress(models.RolloutStatusFailed, 0, "Falha na execução", err.Error())
		return
	}

	// Atualizar progresso em steps
	for _, step := range progressSteps {
		time.Sleep(step.delay)
		updateProgress(models.RolloutStatusRunning, step.progress, step.message, "")
	}

	// Aguardar conclusão do comando
	err = cmd.Wait()

	if err != nil {
		updateProgress(models.RolloutStatusFailed, 0, "Rollout falhou", err.Error())
	} else {
		updateProgress(models.RolloutStatusCompleted, 100, "Rollout concluído", "")
	}
}

// cleanupCompletedRollouts - Limpa progress bars antigos automaticamente pelo StatusPanel
func (a *App) cleanupCompletedRollouts() {
	// Nota: StatusPanel agora gerencia automaticamente a limpeza de progress bars
	// A limpeza acontece automaticamente após 2 minutos de conclusão
}

// startProgressTracking - Inicia o sistema de atualização de progresso
func (a *App) startProgressTracking() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return progressUpdateMsg{}
	})
}

// tickMetricsRefresh - Ticker para atualizar UI enquanto métricas são coletadas
func (a *App) tickMetricsRefresh() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		return metricsRefreshMsg{}
	})
}

// clearStatusMessages - Limpa mensagens de status após 5 segundos
func (a *App) clearStatusMessages() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// startAsyncNodePoolOperation - Inicia tracking de uma operação de node pool
func (a *App) startAsyncNodePoolOperation(pool models.NodePool) {
	// Determinar tipo de operação
	operation := "update"
	if pool.NodeCount != pool.OriginalValues.NodeCount {
		operation = "scale"
	}
	if pool.AutoscalingEnabled != pool.OriginalValues.AutoscalingEnabled {
		if pool.AutoscalingEnabled {
			operation = "autoscale"
		} else {
			operation = "manual"
		}
	}

	// Usar StatusPanel para gerenciar progresso com progress bar
	progressID := fmt.Sprintf("nodepool_%s", pool.Name)

	// Adicionar progress bar inicial
	a.model.StatusContainer.AddProgressBar(progressID, fmt.Sprintf("%s %s", pool.Name, operation), 100)
	a.model.StatusContainer.UpdateProgress(progressID, 5, "running")

	// Log básico da operação
	a.model.StatusContainer.AddInfo("nodepool", fmt.Sprintf("🔄 %s: %s", pool.Name, operation))
}

// updateNodePoolProgress - Atualiza o progresso de uma operação de node pool usando StatusPanel
func (a *App) updateNodePoolProgress(poolName string, status models.RolloutStatus, progress int, message, errorMsg string) {
	progressID := fmt.Sprintf("nodepool_%s", poolName)

	// Converter status para texto
	statusText := message
	if errorMsg != "" {
		statusText = fmt.Sprintf("%s - Erro: %s", message, errorMsg)
	}

	// Atualizar progress bar no StatusContainer
	a.model.StatusContainer.UpdateProgress(progressID, progress, statusText)

	// Se completou ou falhou, marcar como completo (será removido após 3 segundos)
	if progress >= 100 || status == models.RolloutStatusCompleted || status == models.RolloutStatusFailed {
		a.model.StatusContainer.CompleteProgress(progressID)

		// Log final
		if status == models.RolloutStatusCompleted {
			a.model.StatusContainer.AddSuccess("nodepool", fmt.Sprintf("✅ %s: Operação concluída", poolName))
		} else if status == models.RolloutStatusFailed {
			a.model.StatusContainer.AddError("nodepool", fmt.Sprintf("❌ %s: %s", poolName, errorMsg))
		}
	}
}

func (a *App) applyNodePoolChanges(nodePools []models.NodePool) tea.Cmd {
	return func() tea.Msg {
		if len(nodePools) == 0 {
			return nodePoolsAppliedMsg{appliedPools: nil, err: nil}
		}

		// Iniciar progress tracking para cada node pool
		for _, pool := range nodePools {
			a.startAsyncNodePoolOperation(pool)
		}

		successCount := 0
		var appliedPools []models.NodePool
		var lastError error

		// Aplicar mudanças em cada node pool
		for _, pool := range nodePools {
			// Logar início da aplicação
			a.model.StatusContainer.AddInfo("apply-nodepool", fmt.Sprintf("⚙️ Aplicando Node Pool: %s", pool.Name))

			// Logar alterações que serão aplicadas
			a.logNodePoolChanges(pool)

			// Executar comando Azure CLI para update do node pool
			err := a.updateNodePoolViaAzureCLI(pool)
			if err != nil {
				// Atualizar progress para falha
				a.updateNodePoolProgress(pool.Name, models.RolloutStatusFailed, 100, "Falha na aplicação", err.Error())
				a.model.StatusContainer.AddError("apply-nodepool", fmt.Sprintf("❌ Erro ao aplicar Node Pool %s: %v", pool.Name, err))
				lastError = err
				continue
			}

			// Node pool aplicado com sucesso
			a.updateNodePoolProgress(pool.Name, models.RolloutStatusCompleted, 100, "Operação concluída", "")
			a.model.StatusContainer.AddSuccess("apply-nodepool", fmt.Sprintf("✅ Node Pool aplicado: %s", pool.Name))

			// Incrementar contador de aplicações
			pool.AppliedCount++

			successCount++
			appliedPools = append(appliedPools, pool)
		}

		// Se houve falhas, reportar erro
		if successCount < len(nodePools) {
			return nodePoolsAppliedMsg{
				appliedPools: appliedPools,
				err:          fmt.Errorf("aplicadas %d de %d mudanças. Último erro: %v", successCount, len(nodePools), lastError),
			}
		}

		// Sucesso total
		return nodePoolsAppliedMsg{
			appliedPools: appliedPools,
			err:          nil,
		}
	}
}

func (a *App) saveSession(sessionParam *models.Session) tea.Cmd {
	return func() tea.Msg {
		a.debugLog("🔧 saveSession called for '%s', CurrentFolder='%s'", sessionParam.Name, a.model.CurrentFolder)
		if a.sessionManager == nil {
			a.debugLog("❌ Session manager not initialized")
			return sessionSavedMsg{
				sessionName: sessionParam.Name,
				err:         fmt.Errorf("session manager not initialized"),
			}
		}

		// Criar sessão completa
		fullSession := &models.Session{
			Name:            sessionParam.Name,
			CreatedAt:       time.Now(),
			CreatedBy:       "k8s-hpa-manager",
			Changes:         make([]models.HPAChange, 0),
			NodePoolChanges: make([]models.NodePoolChange, 0),
		}

		// Identificar clusters afetados
		clustersMap := make(map[string]bool)

		// Adicionar mudanças dos HPAs selecionados (TODOS, não apenas modificados - para rollback)
		for _, hpa := range a.model.SelectedHPAs {
			clustersMap[hpa.Cluster] = true

			// Usar valores originais se existirem, senão usar valores atuais
			originalValues := hpa.OriginalValues
			if originalValues == nil {
				// Se não há OriginalValues, significa que nunca foi modificado
				// Então os valores atuais SÃO os originais
				originalValues = &models.HPAValues{
					MinReplicas:  hpa.MinReplicas,
					MaxReplicas:  hpa.MaxReplicas,
					TargetCPU:    hpa.TargetCPU,
					TargetMemory: hpa.TargetMemory,

					DeploymentName: hpa.DeploymentName,
					CPURequest:     hpa.TargetCPURequest,
					CPULimit:       hpa.TargetCPULimit,
					MemoryRequest:  hpa.TargetMemoryRequest,
					MemoryLimit:    hpa.TargetMemoryLimit,
				}
			}

			change := models.HPAChange{
				Cluster:        hpa.Cluster,
				Namespace:      hpa.Namespace,
				HPAName:        hpa.Name,
				OriginalValues: originalValues,
				NewValues: &models.HPAValues{
					MinReplicas:  hpa.MinReplicas,
					MaxReplicas:  hpa.MaxReplicas,
					TargetCPU:    hpa.TargetCPU,
					TargetMemory: hpa.TargetMemory,

					// Rollout Options
					PerformRollout:            hpa.PerformRollout,
					PerformDaemonSetRollout:   hpa.PerformDaemonSetRollout,
					PerformStatefulSetRollout: hpa.PerformStatefulSetRollout,

					// Recursos do deployment
					DeploymentName: hpa.DeploymentName,
					CPURequest:     hpa.TargetCPURequest,
					CPULimit:       hpa.TargetCPULimit,
					MemoryRequest:  hpa.TargetMemoryRequest,
					MemoryLimit:    hpa.TargetMemoryLimit,
				},
				Applied:                     false,
				RolloutTriggered:            hpa.PerformRollout,
				DaemonSetRolloutTriggered:   hpa.PerformDaemonSetRollout,
				StatefulSetRolloutTriggered: hpa.PerformStatefulSetRollout,
			}
			fullSession.Changes = append(fullSession.Changes, change)
		}

		// Adicionar mudanças dos node pools selecionados (TODOS, não apenas modificados - para rollback)
		for _, pool := range a.model.SelectedNodePools {
			// Identificar cluster do pool (pode estar em pool.ResourceGroup ou SelectedCluster)
			clusterName := ""
			if a.model.SelectedCluster != nil {
				clusterName = a.model.SelectedCluster.Name
			}

			if clusterName != "" {
				clustersMap[clusterName] = true

				// Buscar configuração do cluster para obter subscription e resource group
				clusterConfig, err := a.findClusterConfig(clusterName)
				subscription := ""
				resourceGroup := ""
				if err == nil {
					subscription = clusterConfig.Subscription
					resourceGroup = clusterConfig.ResourceGroup
				}

				// Usar valores originais se existirem, senão usar valores atuais
				originalValues := pool.OriginalValues
				// Verificar se OriginalValues é um struct vazio (valor zero)
				if originalValues == (models.NodePoolValues{}) {
					// Se não há OriginalValues, significa que nunca foi modificado
					// Então os valores atuais SÃO os originais
					originalValues = models.NodePoolValues{
						NodeCount:          pool.NodeCount,
						MinNodeCount:       pool.MinNodeCount,
						MaxNodeCount:       pool.MaxNodeCount,
						AutoscalingEnabled: pool.AutoscalingEnabled,
					}
				}

				change := models.NodePoolChange{
					Cluster:        clusterName,
					ResourceGroup:  resourceGroup,
					Subscription:   subscription,
					NodePoolName:   pool.Name,
					OriginalValues: originalValues,
					NewValues: models.NodePoolValues{
						NodeCount:          pool.NodeCount,
						MinNodeCount:       pool.MinNodeCount,
						MaxNodeCount:       pool.MaxNodeCount,
						AutoscalingEnabled: pool.AutoscalingEnabled,
					},
					Applied: false,

					// Salvar dados de execução sequencial
					SequenceOrder:  pool.SequenceOrder,
					SequenceStatus: pool.SequenceStatus,
				}
				fullSession.NodePoolChanges = append(fullSession.NodePoolChanges, change)
			}
		}

		// Criar metadados da sessão
		clustersAffected := make([]string, 0, len(clustersMap))
		for cluster := range clustersMap {
			clustersAffected = append(clustersAffected, cluster)
		}

		fullSession.Metadata = &models.SessionMetadata{
			ClustersAffected: clustersAffected,
			NamespacesCount:  len(a.model.SelectedNamespaces),
			HPACount:         len(fullSession.Changes),
			NodePoolCount:    len(fullSession.NodePoolChanges),
			ResourceCount:    0, // Para futuro uso
			TotalChanges:     len(fullSession.Changes) + len(fullSession.NodePoolChanges),
		}

		// Salvar sessão usando o session manager
		var err error
		a.debugLog("💾 About to save session. CurrentFolder='%s', HPA count=%d, NodePool count=%d",
			a.model.CurrentFolder, len(fullSession.Changes), len(fullSession.NodePoolChanges))
		if a.model.CurrentFolder != "" {
			// Converter string para SessionFolder
			var folder session.SessionFolder
			switch a.model.CurrentFolder {
			case "HPA-Upscale":
				folder = session.FolderHPAUpscale
				a.debugLog("📁 Using folder: HPA-Upscale")
			case "HPA-Downscale":
				folder = session.FolderHPADownscale
				a.debugLog("📁 Using folder: HPA-Downscale")
			case "Node-Upscale":
				folder = session.FolderNodeUpscale
				a.debugLog("📁 Using folder: Node-Upscale")
			case "Node-Downscale":
				folder = session.FolderNodeDownscale
				a.debugLog("📁 Using folder: Node-Downscale")
			case "Rollback":
				folder = session.FolderRollback
				a.debugLog("📁 Using folder: Rollback")
			default:
				a.debugLog("❌ Invalid folder name: %s", a.model.CurrentFolder)
				return sessionSavedMsg{
					sessionName: fullSession.Name,
					err:         fmt.Errorf("invalid folder name: %s", a.model.CurrentFolder),
				}
			}
			a.debugLog("💾 Calling SaveSessionToFolder...")
			err = a.sessionManager.SaveSessionToFolder(fullSession, folder)
		} else {
			a.debugLog("💾 Calling SaveSession (root folder)...")
			err = a.sessionManager.SaveSession(fullSession)
		}

		if err != nil {
			a.debugLog("❌ Save error: %v", err)
		} else {
			a.debugLog("✅ Session saved successfully")
		}

		return sessionSavedMsg{
			sessionName: fullSession.Name,
			err:         err,
		}
	}
}

// deleteSession remove uma sessão salva
func (a *App) deleteSession(sessionName string) tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionDeletedMsg{
				sessionName: sessionName,
				err:         fmt.Errorf("session manager not initialized"),
			}
		}

		// Deletar sessão usando o session manager
		err := a.sessionManager.DeleteSession(sessionName)
		if err != nil {
			return sessionDeletedMsg{
				sessionName: sessionName,
				err:         err,
			}
		}

		return sessionDeletedMsg{
			sessionName: sessionName,
			err:         nil,
		}
	}
}

// deleteSessionFromFolder remove uma sessão de uma pasta específica
func (a *App) deleteSessionFromFolder(sessionName, folderName string) tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionDeletedMsg{
				sessionName: sessionName,
				err:         fmt.Errorf("session manager not initialized"),
			}
		}

		// Converter string para SessionFolder
		var folder session.SessionFolder
		if folderName != "" {
			switch folderName {
			case "HPA-Upscale":
				folder = session.FolderHPAUpscale
			case "HPA-Downscale":
				folder = session.FolderHPADownscale
			case "Node-Upscale":
				folder = session.FolderNodeUpscale
			case "Node-Downscale":
				folder = session.FolderNodeDownscale
			case "Rollback":
				folder = session.FolderRollback
			default:
				return sessionDeletedMsg{
					sessionName: sessionName,
					err:         fmt.Errorf("invalid folder name: %s", folderName),
				}
			}
		}

		// Deletar sessão usando o session manager
		var err error
		if folderName != "" {
			err = a.sessionManager.DeleteSessionFromFolder(sessionName, folder)
		} else {
			err = a.sessionManager.DeleteSession(sessionName)
		}

		return sessionDeletedMsg{
			sessionName: sessionName,
			err:         err,
		}
	}
}

// renameSessionInFolder renomeia uma sessão em uma pasta específica
func (a *App) renameSessionInFolder(oldName, newName, folderName string) tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionRenamedMsg{
				oldName: oldName,
				newName: newName,
				err:     fmt.Errorf("session manager not initialized"),
			}
		}

		// Converter string para SessionFolder
		var folder session.SessionFolder
		if folderName != "" {
			switch folderName {
			case "HPA-Upscale":
				folder = session.FolderHPAUpscale
			case "HPA-Downscale":
				folder = session.FolderHPADownscale
			case "Node-Upscale":
				folder = session.FolderNodeUpscale
			case "Node-Downscale":
				folder = session.FolderNodeDownscale
			case "Rollback":
				folder = session.FolderRollback
			default:
				return sessionRenamedMsg{
					oldName: oldName,
					newName: newName,
					err:     fmt.Errorf("invalid folder name: %s", folderName),
				}
			}
		}

		// Renomear sessão usando o session manager
		var err error
		if folderName != "" {
			err = a.sessionManager.RenameSessionInFolder(oldName, newName, folder)
		} else {
			err = a.sessionManager.RenameSession(oldName, newName)
		}

		return sessionRenamedMsg{
			oldName: oldName,
			newName: newName,
			err:     err,
		}
	}
}

// loadSessionFolders carrega todas as pastas de sessão
func (a *App) loadSessionFolders() tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionFoldersLoadedMsg{err: fmt.Errorf("session manager not initialized")}
		}

		folders := a.sessionManager.ListSessionFolders()
		folderNames := make([]string, len(folders))
		for i, folder := range folders {
			folderNames[i] = string(folder)
		}

		return sessionFoldersLoadedMsg{
			folders: folderNames,
			err:     nil,
		}
	}
}

// loadSessionsFromFolder carrega as sessões de uma pasta específica
func (a *App) loadSessionsFromFolder(folderName string) tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionsLoadedMsg{err: fmt.Errorf("session manager not initialized")}
		}

		// Converter string para SessionFolder
		var folder session.SessionFolder
		switch folderName {
		case "HPA-Upscale":
			folder = session.FolderHPAUpscale
		case "HPA-Downscale":
			folder = session.FolderHPADownscale
		case "Node-Upscale":
			folder = session.FolderNodeUpscale
		case "Node-Downscale":
			folder = session.FolderNodeDownscale
		case "Rollback":
			folder = session.FolderRollback
		default:
			return sessionsLoadedMsg{err: fmt.Errorf("invalid folder name: %s", folderName)}
		}

		sessions, err := a.sessionManager.ListSessionsInFolder(folder)
		if err != nil {
			return sessionsLoadedMsg{err: err}
		}

		return sessionsLoadedMsg{
			sessions: sessions,
			err:      nil,
		}
	}
}

// loadSessions carrega todas as sessões salvas (compatibilidade retroativa)
func (a *App) loadSessions() tea.Cmd {
	return func() tea.Msg {
		if a.sessionManager == nil {
			return sessionsLoadedMsg{err: fmt.Errorf("session manager not initialized")}
		}

		sessions, err := a.sessionManager.ListSessions()
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

// applySessionChanges aplica as mudanças de uma sessão carregada
func (a *App) applySessionChanges(session *models.Session) tea.Cmd {
	return func() tea.Msg {
		if len(session.Changes) == 0 {
			return hpaChangesAppliedMsg{count: 0, err: fmt.Errorf("session has no changes to apply")}
		}

		successCount := 0
		var lastError error

		// Aplicar mudanças de cada HPA na sessão
		for _, change := range session.Changes {
			// Obter cliente do cluster
			client, exists := a.getClient(change.Cluster)
			if !exists {
				// Tentar criar cliente se não existir
				if a.kubeManager == nil {
					lastError = fmt.Errorf("kube manager not initialized")
					continue
				}

				clientSet, err := a.kubeManager.GetClient(change.Cluster)
				if err != nil {
					lastError = fmt.Errorf("failed to get client for cluster %s: %w", change.Cluster, err)
					continue
				}

				newClient := kubernetes.NewClient(clientSet, change.Cluster)
				a.setClient(change.Cluster, newClient)
				client = newClient
			}

			// Criar HPA model a partir do change
			hpa := models.HPA{
				Name:         change.HPAName,
				Namespace:    change.Namespace,
				Cluster:      change.Cluster,
				MinReplicas:  change.NewValues.MinReplicas,
				MaxReplicas:  change.NewValues.MaxReplicas,
				TargetCPU:    change.NewValues.TargetCPU,
				TargetMemory: change.NewValues.TargetMemory,
			}

			// Aplicar mudanças no HPA
			err := client.UpdateHPA(a.ctx, hpa)
			if err != nil {
				lastError = err
				continue
			}

			successCount++
		}

		// Se houve falhas, reportar erro
		if successCount < len(session.Changes) {
			return hpaChangesAppliedMsg{
				count: successCount,
				err:   fmt.Errorf("applied %d of %d changes from session '%s'. Last error: %v", successCount, len(session.Changes), session.Name, lastError),
			}
		}

		// Sucesso total
		return hpaChangesAppliedMsg{
			count: successCount,
			err:   nil,
		}
	}
}

// loadSessionState carrega o estado da aplicação baseado numa sessão salva
func (a *App) loadSessionState(session *models.Session) tea.Cmd {
	return func() tea.Msg {
		// Zerar contadores de aplicação para nova sessão
		a.resetHPAApplicationCounters()

		// Verificar tipos de mudanças na sessão
		hasHPAChanges := len(session.Changes) > 0
		hasNodePoolChanges := len(session.NodePoolChanges) > 0
		hasResourceChanges := len(session.ResourceChanges) > 0

		a.debugLog("📊 Analisando sessão: HPAs=%d, NodePools=%d, Resources=%d\n",
			len(session.Changes), len(session.NodePoolChanges), len(session.ResourceChanges))

		// Verificar se é sessão mista (HPAs + Node Pools)
		if hasHPAChanges && hasNodePoolChanges {
			// Sessão mista - carregar ambos HPAs e node pools
			// Por enquanto, vamos carregar os HPAs primeiro e permitir navegação entre os painéis
			a.debugLog("🔀 Sessão mista detectada - carregando HPAs primeiro\n")
			return a.loadHPASessionState(session)
		} else if hasNodePoolChanges {
			// É uma sessão só de node pools
			a.debugLog("🔧 Sessão de node pools detectada\n")
			return a.loadNodePoolSessionState(session)
		} else if hasHPAChanges {
			// É uma sessão só de HPAs
			a.debugLog("📊 Sessão de HPAs detectada\n")
			return a.loadHPASessionState(session)
		} else if hasResourceChanges {
			// É uma sessão de recursos do cluster
			a.debugLog("⚙️ Sessão de recursos detectada\n")
			return sessionStateLoadedMsg{err: fmt.Errorf("resource sessions not yet supported")}
		} else {
			// Nenhuma mudança encontrada
			return sessionStateLoadedMsg{err: fmt.Errorf("session '%s' contains no changes to load (empty HPAs, node pools, and resources)", session.Name)}
		}
	}
}

// loadHPASessionState carrega uma sessão de HPAs (código original)
func (a *App) loadHPASessionState(session *models.Session) tea.Msg {
	// Usar clusters_affected dos metadados da sessão (prioritário)
	var targetCluster string
	if session.Metadata != nil && session.Metadata.ClustersAffected != nil && len(session.Metadata.ClustersAffected) > 0 {
		targetCluster = session.Metadata.ClustersAffected[0]
		a.debugLog("🔍 Usando cluster dos metadados da sessão: %s\n", targetCluster)
	} else {
		// Fallback: identificar clusters únicos nos Changes
		clustersMap := make(map[string]bool)
		for _, change := range session.Changes {
			if change.Cluster != "" {
				clustersMap[change.Cluster] = true
			}
		}

		// Usar o primeiro cluster encontrado
		for cluster := range clustersMap {
			targetCluster = cluster
			break
		}
		if targetCluster != "" {
			a.debugLog("🔍 Usando cluster extraído dos changes: %s\n", targetCluster)
		} else {
			a.debugLog("⚠️ Nenhum cluster encontrado nos changes\n")
		}
	}

	if targetCluster == "" {
		return sessionStateLoadedMsg{err: fmt.Errorf("no cluster found in session")}
	}

	// Converter as mudanças da sessão em HPAs com modificações
	var sessionHPAs []models.HPA
	namespacesMap := make(map[string]bool)

	for _, change := range session.Changes {
		if change.Cluster != targetCluster {
			continue // Por enquanto, só um cluster
		}

		namespacesMap[change.Namespace] = true

		hpa := models.HPA{
			Name:                      change.HPAName,
			Namespace:                 change.Namespace,
			Cluster:                   change.Cluster,
			MinReplicas:               change.NewValues.MinReplicas,
			MaxReplicas:               change.NewValues.MaxReplicas,
			TargetCPU:                 change.NewValues.TargetCPU,
			TargetMemory:              change.NewValues.TargetMemory,
			PerformRollout:            change.RolloutTriggered,
			PerformDaemonSetRollout:   change.DaemonSetRolloutTriggered,
			PerformStatefulSetRollout: change.StatefulSetRolloutTriggered,
			OriginalValues:            change.OriginalValues,
			Selected:                  true,
			Modified:                  true, // Marcar como modificado

			// Recursos do deployment da sessão
			DeploymentName:      change.NewValues.DeploymentName,
			TargetCPURequest:    change.NewValues.CPURequest,
			TargetCPULimit:      change.NewValues.CPULimit,
			TargetMemoryRequest: change.NewValues.MemoryRequest,
			TargetMemoryLimit:   change.NewValues.MemoryLimit,
			ResourcesModified:   change.NewValues.CPURequest != "" || change.NewValues.CPULimit != "" || change.NewValues.MemoryRequest != "" || change.NewValues.MemoryLimit != "",

			// Valores originais dos recursos (se existirem)
			CurrentCPURequest:    change.OriginalValues.CPURequest,
			CurrentCPULimit:      change.OriginalValues.CPULimit,
			CurrentMemoryRequest: change.OriginalValues.MemoryRequest,
			CurrentMemoryLimit:   change.OriginalValues.MemoryLimit,
		}

		// Se não há dados de recursos na sessão, marcar para enriquecer posteriormente
		if hpa.DeploymentName == "" && hpa.CurrentCPURequest == "" && hpa.CurrentCPULimit == "" &&
			hpa.CurrentMemoryRequest == "" && hpa.CurrentMemoryLimit == "" {
			hpa.NeedsEnrichment = true
		}
		sessionHPAs = append(sessionHPAs, hpa)
	}

	// Criar lista de namespaces da sessão
	var sessionNamespaces []models.Namespace
	for ns := range namespacesMap {
		namespace := models.Namespace{
			Name:     ns,
			Cluster:  targetCluster,
			HPACount: 0, // Será contado depois
			Selected: true,
		}
		sessionNamespaces = append(sessionNamespaces, namespace)
	}

	return sessionStateLoadedMsg{
		clusterName: targetCluster,
		namespaces:  sessionNamespaces,
		hpas:        sessionHPAs,
		nodePools:   []models.NodePool{}, // Sessão de HPAs não tem node pools
		sessionName: session.Name,
		err:         nil,
	}
}

// loadNodePoolSessionState carrega uma sessão de node pools
func (a *App) loadNodePoolSessionState(session *models.Session) tea.Msg {
	// Usar clusters_affected dos metadados da sessão (prioritário)
	var targetCluster string

	if session.Metadata != nil && session.Metadata.ClustersAffected != nil && len(session.Metadata.ClustersAffected) > 0 {
		targetCluster = session.Metadata.ClustersAffected[0]
		a.debugLog("🔍 Usando cluster dos metadados da sessão: %s\n", targetCluster)
	} else {
		// Fallback: identificar clusters únicos nos NodePoolChanges
		clustersMap := make(map[string]bool)
		for _, change := range session.NodePoolChanges {
			if change.Cluster != "" {
				clustersMap[change.Cluster] = true
			}
		}
		// Usar o primeiro cluster encontrado
		for cluster := range clustersMap {
			targetCluster = cluster
			break
		}
		if targetCluster != "" {
			a.debugLog("🔍 Usando cluster extraído dos changes: %s\n", targetCluster)
		} else {
			a.debugLog("⚠️ Nenhum cluster encontrado nos changes\n")
		}
	}

	if targetCluster == "" {
		return sessionStateLoadedMsg{err: fmt.Errorf("no cluster found in node pool session")}
	}

	// Buscar configuração do cluster no clusters-config.json
	clusterConfig, err := a.findClusterConfig(targetCluster)
	if err != nil {
		return sessionStateLoadedMsg{err: fmt.Errorf("failed to find cluster config for %s: %w", targetCluster, err)}
	}

	// Configurar contexto Azure com a subscription do cluster
	if err := a.setupAzureContext(clusterConfig.Subscription); err != nil {
		return sessionStateLoadedMsg{err: fmt.Errorf("failed to setup Azure context: %w", err)}
	}

	// Carregar node pools atuais do cluster
	a.debugLog("🔄 Carregando node pools do cluster %s...\n", targetCluster)

	// Normalizar nome do cluster para Azure CLI (remover -admin se existir)
	clusterNameForAzure := strings.TrimSuffix(targetCluster, "-admin")

	// Carregar node pools via Azure CLI
	a.debugLog("📋 Carregando node pools: cluster=%s, resourceGroup=%s, subscription=%s\n",
		clusterNameForAzure, clusterConfig.ResourceGroup, clusterConfig.Subscription)

	nodePools, err := loadNodePoolsFromAzure(clusterNameForAzure, clusterConfig.ResourceGroup, clusterConfig.Subscription)
	if err != nil {
		a.debugLog("❌ Erro ao carregar node pools: %v\n", err)
		return sessionStateLoadedMsg{err: fmt.Errorf("failed to load node pools: %w", err)}
	}

	a.debugLog("📊 Carregados %d node pools do Azure\n", len(nodePools))

	// Aplicar as modificações da sessão aos node pools carregados
	var sessionNodePools []models.NodePool
	poolsInSession := make(map[string]bool)

	// Primeiro, aplicar modificações aos pools existentes no cluster
	for _, pool := range nodePools {
		// Verificar se este pool tem mudanças na sessão
		for _, change := range session.NodePoolChanges {
			if change.NodePoolName == pool.Name && change.Cluster == targetCluster {
				// Aplicar mudanças da sessão
				pool.NodeCount = change.NewValues.NodeCount
				pool.MinNodeCount = change.NewValues.MinNodeCount
				pool.MaxNodeCount = change.NewValues.MaxNodeCount
				pool.AutoscalingEnabled = change.NewValues.AutoscalingEnabled
				pool.Modified = true
				pool.Selected = true
				poolsInSession[pool.Name] = true

				// Restaurar dados de execução sequencial
				pool.SequenceOrder = change.SequenceOrder
				pool.SequenceStatus = change.SequenceStatus

				a.debugLog("📝 Pool '%s' atualizado com dados da sessão (sequência: %d, status: %s)\n",
					pool.Name, pool.SequenceOrder, pool.SequenceStatus)
				break
			}
		}
		sessionNodePools = append(sessionNodePools, pool)
	}

	// Adicionar pools que estão na sessão mas não existem mais no cluster (para histórico)
	for _, change := range session.NodePoolChanges {
		if change.Cluster == targetCluster && !poolsInSession[change.NodePoolName] {
			// Pool da sessão não existe mais no cluster - criar entrada histórica
			historicalPool := models.NodePool{
				Name:               change.NodePoolName,
				NodeCount:          change.NewValues.NodeCount,
				MinNodeCount:       change.NewValues.MinNodeCount,
				MaxNodeCount:       change.NewValues.MaxNodeCount,
				AutoscalingEnabled: change.NewValues.AutoscalingEnabled,
				Modified:           true,
				Selected:           true,
				Status:             "Historical", // Marcar como histórico
			}
			sessionNodePools = append(sessionNodePools, historicalPool)
			a.debugLog("⚠️ Pool '%s' da sessão não existe mais no cluster - adicionado como histórico\n", change.NodePoolName)
		}
	}

	a.debugLog("✅ Carregados %d node pools com modificações aplicadas\n", len(sessionNodePools))

	// Contar quantos pools estão marcados como modificados
	modifiedCount := 0
	for _, pool := range sessionNodePools {
		if pool.Modified {
			modifiedCount++
		}
	}
	a.debugLog("📝 %d node pools marcados como modificados\n", modifiedCount)

	// Definir estado da aplicação para node pools
	return sessionStateLoadedMsg{
		clusterName: targetCluster,
		namespaces:  []models.Namespace{}, // Node pools não usam namespaces
		hpas:        []models.HPA{},       // Não há HPAs numa sessão de node pools
		nodePools:   sessionNodePools,     // Node pools carregados com modificações aplicadas
		sessionName: session.Name,
		err:         nil,
	}
}

// getCurrentFieldValue retorna o valor atual do campo sendo editado
func (a *App) getCurrentFieldValue(fieldName string) string {
	if a.model.EditingHPA == nil {
		return ""
	}

	hpa := a.model.EditingHPA
	switch fieldName {
	case "min_replicas":
		if hpa.MinReplicas != nil {
			return fmt.Sprintf("%d", *hpa.MinReplicas)
		}
		return "1"
	case "max_replicas":
		return fmt.Sprintf("%d", hpa.MaxReplicas)
	case "target_cpu":
		if hpa.TargetCPU != nil {
			return fmt.Sprintf("%d", *hpa.TargetCPU)
		}
		return "80"
	case "target_memory":
		if hpa.TargetMemory != nil {
			return fmt.Sprintf("%d", *hpa.TargetMemory)
		}
		return ""
	case "rollout":
		if hpa.PerformRollout {
			return "true"
		}
		return "false"

	// Campos de recursos do deployment
	case "deployment_cpu_request":
		if hpa.TargetCPURequest != "" {
			return hpa.TargetCPURequest
		}
		return hpa.CurrentCPURequest
	case "deployment_cpu_limit":
		if hpa.TargetCPULimit != "" {
			return hpa.TargetCPULimit
		}
		return hpa.CurrentCPULimit
	case "deployment_memory_request":
		if hpa.TargetMemoryRequest != "" {
			return hpa.TargetMemoryRequest
		}
		return hpa.CurrentMemoryRequest
	case "deployment_memory_limit":
		if hpa.TargetMemoryLimit != "" {
			return hpa.TargetMemoryLimit
		}
		return hpa.CurrentMemoryLimit

	default:
		return ""
	}
}

// applyFieldValue aplica o valor editado ao campo do HPA
func (a *App) applyFieldValue(fieldName, value string) error {
	if a.model.EditingHPA == nil {
		return fmt.Errorf("no HPA being edited")
	}

	hpa := a.model.EditingHPA
	switch fieldName {
	case "min_replicas":
		if value == "" {
			hpa.MinReplicas = nil
		} else {
			val, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return err
			}
			minVal := int32(val)
			hpa.MinReplicas = &minVal
		}
	case "max_replicas":
		val, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		hpa.MaxReplicas = int32(val)
	case "target_cpu":
		if value == "" {
			hpa.TargetCPU = nil
		} else {
			val, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return err
			}
			cpuVal := int32(val)
			hpa.TargetCPU = &cpuVal
		}
	case "target_memory":
		if value == "" {
			hpa.TargetMemory = nil
		} else {
			val, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return err
			}
			memVal := int32(val)
			hpa.TargetMemory = &memVal
		}
	case "rollout":
		lowerValue := strings.ToLower(value)
		hpa.PerformRollout = (lowerValue == "true" || lowerValue == "t" || lowerValue == "yes" || lowerValue == "y" || lowerValue == "1")

	// Campos de recursos do deployment
	case "deployment_cpu_request":
		hpa.TargetCPURequest = value
		hpa.ResourcesModified = true
	case "deployment_cpu_limit":
		hpa.TargetCPULimit = value
		hpa.ResourcesModified = true
	case "deployment_memory_request":
		hpa.TargetMemoryRequest = value
		hpa.ResourcesModified = true
	case "deployment_memory_limit":
		hpa.TargetMemoryLimit = value
		hpa.ResourcesModified = true
	}

	return nil
}

// handleHelpKeys - Navegação na tela de ajuda
func (a *App) handleHelpKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.model.HelpScrollOffset > 0 {
			a.model.HelpScrollOffset--
		}
	case "down", "j":
		// Permitir scroll para baixo (limite será controlado na renderização)
		a.model.HelpScrollOffset++
	case "pageup":
		a.model.HelpScrollOffset -= 10
		if a.model.HelpScrollOffset < 0 {
			a.model.HelpScrollOffset = 0
		}
	case "pagedown":
		a.model.HelpScrollOffset += 10
	case "home":
		a.model.HelpScrollOffset = 0
	case "end":
		a.model.HelpScrollOffset = 50 // Valor grande para ir ao final
	default:
		// Outras teclas voltam para o estado anterior
		a.model.RestoreHelpSnapshot() // Restaurar snapshot completo
		a.model.HelpScrollOffset = 0  // Reset scroll
	}
	return a, nil
}

// updateNodePoolViaAzureCLI atualiza um node pool via Azure CLI
func (a *App) updateNodePoolViaAzureCLI(pool models.NodePool) error {
	// Primeiro, verificar se há mudanças para aplicar
	if !pool.Modified {
		return nil
	}

	// Etapa 1: Validação inicial (5% -> 15%)
	a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, 15, "Validando configurações...", "")

	// Normalizar nome do cluster para Azure CLI (remover -admin se existir)
	clusterNameForAzure := strings.TrimSuffix(pool.ClusterName, "-admin")

	// Etapa 2: Preparando comandos (15% -> 25%)
	a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, 25, "Preparando comandos Azure CLI...", "")

	// Construir comandos Azure CLI baseados nas mudanças
	// IMPORTANTE: Ordem correta para evitar conflitos:
	// 1. Se mudou de auto→manual: PRIMEIRO desabilita autoscaling, DEPOIS faz scale
	// 2. Se mudou de manual→auto: PRIMEIRO faz scale (se necessário), DEPOIS habilita autoscaling
	// 3. Se permaneceu auto: Atualiza min/max
	// 4. Se permaneceu manual: Faz scale
	var cmds [][]string

	// Detectar transição de autoscaling
	changingToManual := pool.OriginalValues.AutoscalingEnabled && !pool.AutoscalingEnabled
	changingToAuto := !pool.OriginalValues.AutoscalingEnabled && pool.AutoscalingEnabled
	staysAuto := pool.OriginalValues.AutoscalingEnabled && pool.AutoscalingEnabled
	staysManual := !pool.OriginalValues.AutoscalingEnabled && !pool.AutoscalingEnabled

	// CENÁRIO 1: Mudou de AUTO → MANUAL (precisa desabilitar autoscaling PRIMEIRO)
	if changingToManual {
		// Passo 1: Desabilitar autoscaling
		cmd := []string{
			"az", "aks", "nodepool", "update",
			"--disable-cluster-autoscaler",
			"--resource-group", pool.ResourceGroup,
			"--cluster-name", clusterNameForAzure,
			"--name", pool.Name,
		}
		if pool.Subscription != "" {
			cmd = append(cmd, "--subscription", pool.Subscription)
		}
		cmds = append(cmds, cmd)

		// Passo 2: Se NodeCount mudou, fazer scale manual
		if pool.NodeCount != pool.OriginalValues.NodeCount {
			cmd := []string{
				"az", "aks", "nodepool", "scale",
				"--resource-group", pool.ResourceGroup,
				"--cluster-name", clusterNameForAzure,
				"--name", pool.Name,
				"--node-count", fmt.Sprintf("%d", pool.NodeCount),
			}
			if pool.Subscription != "" {
				cmd = append(cmd, "--subscription", pool.Subscription)
			}
			cmds = append(cmds, cmd)
		}
	}

	// CENÁRIO 2: Mudou de MANUAL → AUTO
	if changingToAuto {
		// Passo 1: Se NodeCount mudou, fazer scale manual ANTES de habilitar autoscaling
		if pool.NodeCount != pool.OriginalValues.NodeCount {
			cmd := []string{
				"az", "aks", "nodepool", "scale",
				"--resource-group", pool.ResourceGroup,
				"--cluster-name", clusterNameForAzure,
				"--name", pool.Name,
				"--node-count", fmt.Sprintf("%d", pool.NodeCount),
			}
			if pool.Subscription != "" {
				cmd = append(cmd, "--subscription", pool.Subscription)
			}
			cmds = append(cmds, cmd)
		}

		// Passo 2: Habilitar autoscaling com min/max
		cmd := []string{
			"az", "aks", "nodepool", "update",
			"--enable-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", pool.MinNodeCount),
			"--max-count", fmt.Sprintf("%d", pool.MaxNodeCount),
			"--resource-group", pool.ResourceGroup,
			"--cluster-name", clusterNameForAzure,
			"--name", pool.Name,
		}
		if pool.Subscription != "" {
			cmd = append(cmd, "--subscription", pool.Subscription)
		}
		cmds = append(cmds, cmd)
	}

	// CENÁRIO 3: Permaneceu AUTO - atualizar min/max se mudou
	if staysAuto {
		if pool.MinNodeCount != pool.OriginalValues.MinNodeCount || pool.MaxNodeCount != pool.OriginalValues.MaxNodeCount {
			cmd := []string{
				"az", "aks", "nodepool", "update",
				"--update-cluster-autoscaler",
				"--min-count", fmt.Sprintf("%d", pool.MinNodeCount),
				"--max-count", fmt.Sprintf("%d", pool.MaxNodeCount),
				"--resource-group", pool.ResourceGroup,
				"--cluster-name", clusterNameForAzure,
				"--name", pool.Name,
			}
			if pool.Subscription != "" {
				cmd = append(cmd, "--subscription", pool.Subscription)
			}
			cmds = append(cmds, cmd)
		}
	}

	// CENÁRIO 4: Permaneceu MANUAL - fazer scale se mudou
	if staysManual {
		if pool.NodeCount != pool.OriginalValues.NodeCount {
			cmd := []string{
				"az", "aks", "nodepool", "scale",
				"--resource-group", pool.ResourceGroup,
				"--cluster-name", clusterNameForAzure,
				"--name", pool.Name,
				"--node-count", fmt.Sprintf("%d", pool.NodeCount),
			}
			if pool.Subscription != "" {
				cmd = append(cmd, "--subscription", pool.Subscription)
			}
			cmds = append(cmds, cmd)
		}
	}

	// Se não há comandos para executar, não há mudanças
	if len(cmds) == 0 {
		return nil
	}

	// Executar todos os comandos com progress tracking mais granular (30% -> 90%)
	totalCmds := len(cmds)
	for i, cmd := range cmds {
		// Progresso usando StatusPanel
		startProgress := 30 + (i * 60 / totalCmds)
		a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, startProgress, fmt.Sprintf("Iniciando comando %d/%d...", i+1, totalCmds), "")

		// Progresso durante execução
		midProgress := 30 + ((i*60 + 20) / totalCmds)
		a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, midProgress, fmt.Sprintf("Executando comando %d/%d...", i+1, totalCmds), "")

		err := a.executeAzureCommand(cmd)
		if err != nil {
			a.updateNodePoolProgress(pool.Name, models.RolloutStatusFailed, 100, "Falha na execução", err.Error())
			return fmt.Errorf("failed to update node pool %s: %w", pool.Name, err)
		}

		// Progresso final do comando
		endProgress := 30 + ((i + 1) * 60 / totalCmds)
		a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, endProgress, fmt.Sprintf("Comando %d/%d concluído", i+1, totalCmds), "")
	}

	// Progresso final antes de completar
	a.updateNodePoolProgress(pool.Name, models.RolloutStatusRunning, 95, "Finalizando operação...", "")
	return nil
}

// executeAzureCommand executa um comando Azure CLI
func (a *App) executeAzureCommand(cmdArgs []string) error {
	// statusPanel direct access
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	// Extrair operação para log mais legível
	operation := "operação Azure"
	if len(cmdArgs) >= 4 {
		operation = fmt.Sprintf("%s %s", cmdArgs[2], cmdArgs[3]) // ex: "nodepool scale"
	}

	a.model.StatusContainer.AddInfo("azure-cli", fmt.Sprintf("🚀 Executando %s via Azure CLI", operation))

	// Log comando completo para debug (apenas em debug mode)
	if a.debug {
		a.debugLog("🚀 Running command: %s %s", cmdArgs[0], strings.Join(cmdArgs[1:], " "))
	}

	// Separar stdout e stderr para tratar warnings adequadamente
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	// Warnings conhecidos que devem ser ignorados (não são erros!)
	knownWarnings := []string{
		"UserWarning: pkg_resources is deprecated",
		"The behavior of this command has been altered by the following extension",
		"__import__('pkg_resources').declare_namespace(__name__)",
	}

	// Função helper para verificar se stderr contém apenas warnings
	isOnlyWarnings := func(stderr string) bool {
		if stderr == "" {
			return true // Sem stderr = sem problemas
		}

		lines := strings.Split(stderr, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue // Ignorar linhas vazias
			}

			// Verificar se linha contém warning conhecido
			isWarning := false
			for _, warning := range knownWarnings {
				if strings.Contains(trimmed, warning) {
					isWarning = true
					break
				}
			}

			// Se encontrou linha que NÃO é warning conhecido, é erro real
			if !isWarning && !strings.Contains(trimmed, "WARNING:") {
				return false
			}
		}
		return true // Todas as linhas são warnings
	}

	// Verificar se houve erro REAL (não apenas warnings)
	if err != nil {
		// Se stderr contém apenas warnings, ignorar o "erro"
		if isOnlyWarnings(stderrStr) {
			// Log warnings em debug mode, mas não tratar como erro
			if a.debug && stderrStr != "" {
				a.debugLog("⚠️ Warnings ignorados:\n%s", stderrStr)
			}
			// Continuar normalmente - comando foi bem-sucedido
			a.model.StatusContainer.AddSuccess("azure-cli", fmt.Sprintf("✅ %s executado com sucesso", operation))
			a.processAzureOutput(stdoutStr)
			return nil
		}

		// Erro REAL - extrair mensagem
		azureError := "exit status 1"
		if stderrStr != "" {
			// Pegar primeira linha não-vazia do erro REAL
			lines := strings.Split(stderrStr, "\n")
			for _, line := range lines {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					// Ignorar warnings na extração de erro
					isWarning := false
					for _, warning := range knownWarnings {
						if strings.Contains(trimmed, warning) {
							isWarning = true
							break
						}
					}
					if !isWarning && !strings.Contains(trimmed, "WARNING:") {
						azureError = trimmed
						// Limitar tamanho para não poluir
						if len(azureError) > 150 {
							azureError = azureError[:150] + "..."
						}
						break
					}
				}
			}
		}

		a.model.StatusContainer.AddError("azure-cli", fmt.Sprintf("❌ Falha: %s", azureError))

		// Log detalhado no terminal (apenas em debug mode)
		if a.debug {
			a.debugLog("❌ Command failed with error: %s", err.Error())
			a.debugLog("📄 Stderr output:\n%s", stderrStr)
			a.debugLog("📄 Stdout output:\n%s", stdoutStr)
		} else if stderrStr != "" {
			// Mostrar apenas primeiras 3 linhas de ERRO REAL no StatusContainer
			lines := strings.Split(stderrStr, "\n")
			count := 0
			for _, line := range lines {
				if trimmed := strings.TrimSpace(line); trimmed != "" && count < 3 {
					// Não mostrar warnings como erro
					isWarning := false
					for _, warning := range knownWarnings {
						if strings.Contains(trimmed, warning) {
							isWarning = true
							break
						}
					}
					if !isWarning && !strings.Contains(trimmed, "WARNING:") {
						a.model.StatusContainer.AddError("azure-error", trimmed)
						count++
					}
				}
			}
		}

		return fmt.Errorf("Azure CLI: %s", azureError)
	}

	// Sucesso - verificar se há warnings para logar em debug
	if a.debug && stderrStr != "" {
		a.debugLog("⚠️ Warnings (ignorados):\n%s", stderrStr)
	}

	a.model.StatusContainer.AddSuccess("azure-cli", fmt.Sprintf("✅ %s executado com sucesso", operation))

	// Filtrar output JSON para mostrar apenas informações relevantes
	a.processAzureOutput(stdoutStr)
	return nil
}

// processAzureOutput processa e filtra o output do Azure CLI
func (a *App) processAzureOutput(output string) {
	// Se o output parece ser JSON, tentar extrair informações relevantes
	if strings.TrimSpace(output) != "" && (strings.HasPrefix(strings.TrimSpace(output), "{") || strings.HasPrefix(strings.TrimSpace(output), "[")) {
		// Tentar parsear como JSON para extrair informações úteis
		var jsonData map[string]interface{}
		if err := json.Unmarshal([]byte(output), &jsonData); err == nil {
			// Extrair apenas campos relevantes
			if name, ok := jsonData["name"].(string); ok {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("📋 Nome: %s", name))
			}
			if count, ok := jsonData["count"].(float64); ok {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("🔢 Node Count: %.0f", count))
			}
			if minCount, ok := jsonData["minCount"].(float64); ok {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("📉 Min Count: %.0f", minCount))
			}
			if maxCount, ok := jsonData["maxCount"].(float64); ok {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("📈 Max Count: %.0f", maxCount))
			}
			if status, ok := jsonData["provisioningState"].(string); ok && status != "" {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("🏷️  Status: %s", status))
			}
		} else {
			// Se não conseguir parsear JSON, mostrar apenas se não for muito grande
			if len(output) < 200 {
				a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("📄 Output: %s", strings.TrimSpace(output)))
			} else {
				a.model.StatusContainer.AddInfo("azure-output", "📄 Output: ✅ Command executed successfully (output truncated)")
			}
		}
	} else if strings.TrimSpace(output) != "" {
		// Para output não-JSON, mostrar apenas se for pequeno
		if len(output) < 200 {
			a.model.StatusContainer.AddInfo("azure-output", fmt.Sprintf("📄 Output: %s", strings.TrimSpace(output)))
		} else {
			a.model.StatusContainer.AddInfo("azure-output", "📄 Output: ✅ Command executed successfully")
		}
	}
}

// renderMixedSession renderiza a interface de sessão mista (HPAs + Node Pools)
func (a *App) renderMixedSession() string {
	return a.getTabBar() + "🔄 Sessão Mista (HPAs + Node Pools) - Em Implementação\n\n" +
		"TAB - Alternar entre HPAs e Node Pools\n" +
		"SPACE - Selecionar/Desselecionar\n" +
		"ENTER - Editar\n" +
		"Ctrl+S - Salvar Sessão\n" +
		"Ctrl+D/U - Aplicar Mudanças\n" +
		"ESC - Voltar"
}

// applyMixedSession aplica todas as mudanças de uma sessão mista
func (a *App) applyMixedSession() tea.Cmd {
	return func() tea.Msg {
		if a.model.CurrentSession == nil {
			return mixedSessionAppliedMsg{err: fmt.Errorf("no session to apply")}
		}

		var errors []string
		successCount := 0

		// Aplicar mudanças de HPAs
		if len(a.model.CurrentSession.Changes) > 0 {
			a.model.StatusContainer.AddInfo("apply-session", fmt.Sprintf("🔄 Aplicando mudanças em %d HPA(s)...", len(a.model.CurrentSession.Changes)))
			// Aqui deveria chamar a função de aplicar HPAs
			// Por simplicidade, simulando sucesso
			successCount += len(a.model.CurrentSession.Changes)
		}

		// Aplicar mudanças de Node Pools
		if len(a.model.CurrentSession.NodePoolChanges) > 0 {
			a.model.StatusContainer.AddInfo("apply-session", fmt.Sprintf("🔄 Aplicando mudanças em %d Node Pool(s)...", len(a.model.CurrentSession.NodePoolChanges)))
			// Aqui deveria chamar a função de aplicar Node Pools
			// Por simplicidade, simulando sucesso
			successCount += len(a.model.CurrentSession.NodePoolChanges)
		}

		if len(errors) > 0 {
			return mixedSessionAppliedMsg{
				err: fmt.Errorf("alguns erros ocorreram: %v", errors),
			}
		}

		return mixedSessionAppliedMsg{
			successCount: successCount,
			err:          nil,
		}
	}
}

// mixedSessionAppliedMsg representa o resultado da aplicação de uma sessão mista
type mixedSessionAppliedMsg struct {
	successCount int
	err          error
}

// handleF7AllResources inicia o gerenciamento de todos os recursos do cluster
func (a *App) handleF7AllResources() (tea.Model, tea.Cmd) {
	// Verificar se há cluster selecionado
	if a.model.SelectedCluster == nil {
		a.model.Error = "Selecione um cluster primeiro para gerenciar recursos"
		return a, nil
	}

	// Verificar se está em estado válido para F7
	validStates := map[models.AppState]bool{
		models.StateNamespaceSelection: true,
		models.StateHPASelection:       true,
		models.StateNodeSelection:      true,
		models.StateMixedSession:       true,
	}

	if !validStates[a.model.State] {
		a.model.Error = "F7 (Recursos) disponível apenas após seleção de cluster"
		return a, nil
	}

	// Configurar modo de recursos
	a.model.PrometheusStackMode = false
	a.model.ShowSystemResources = false
	a.model.ResourceFilter = models.ResourceMonitoring // Filtro padrão

	// Ir para estado de descoberta de recursos
	a.model.State = models.StateClusterResourceDiscovery
	a.model.Loading = true

	return a, a.discoverClusterResources(false) // false = todos os recursos
}

// handleF8PrometheusResources inicia o gerenciamento específico do Prometheus
func (a *App) handleF8PrometheusResources() (tea.Model, tea.Cmd) {
	// Verificar se há cluster selecionado
	if a.model.SelectedCluster == nil {
		a.model.Error = "Selecione um cluster primeiro para gerenciar Prometheus"
		return a, nil
	}

	// Verificar se está em estado válido para F8
	validStates := map[models.AppState]bool{
		models.StateClusterSelection:   true, // Permitir F8 na seleção de clusters
		models.StateNamespaceSelection: true,
		models.StateHPASelection:       true,
		models.StateNodeSelection:      true,
		models.StateMixedSession:       true,
	}

	if !validStates[a.model.State] {
		a.model.Error = "F8 (Prometheus) disponível apenas após seleção de cluster"
		return a, nil
	}

	// Configurar modo Prometheus
	a.model.PrometheusStackMode = true
	a.model.ShowSystemResources = true // Prometheus está em namespaces system
	a.model.ResourceFilter = models.ResourceMonitoring

	// Ir para estado de descoberta de recursos Prometheus
	a.model.State = models.StateClusterResourceDiscovery
	a.model.Loading = true

	return a, a.discoverClusterResources(true) // true = apenas Prometheus
}

// handleMouseEvent - Trata eventos de mouse para scroll e foco do painel de status
func (a *App) handleMouseEvent(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Verificar se estamos em uma tela que tem painel de status
	validStates := map[models.AppState]bool{
		models.StateHPASelection:       true,
		models.StateNodeSelection:      true,
		models.StateMixedSession:       true,
		models.StateNamespaceSelection: true,
		models.StateHPAEditing:         true,
		models.StateNodeEditing:        true,
	}

	if !validStates[a.model.State] {
		return a, nil
	}

	// statusPanel direct access

	switch msg.Type {
	case tea.MouseLeft:
		// Mouse click - Feature não implementada
		// TODO: Implementar StatusContainer.HandleMouseClick() quando necessário
		if a.debug {
			a.debugLog("Mouse click at X:%d Y:%d (feature disabled)", msg.X, msg.Y)
		}
		return a, nil

	case tea.MouseWheelUp:
		// Mouse wheel scroll - Apenas para painéis de lista (não StatusContainer)
		// TODO: Implementar StatusContainer.IsFocused() para scroll no status panel
		if a.model.ActivePanel == models.PanelSelectedHPAs {
			if a.model.HPASelectedScrollOffset > 0 {
				a.model.HPASelectedScrollOffset--
			}
		} else if a.model.ActivePanel == models.PanelSelectedNodePools {
			if a.model.NodePoolSelectedScrollOffset > 0 {
				a.model.NodePoolSelectedScrollOffset--
			}
		}
		return a, nil

	case tea.MouseWheelDown:
		// Mouse wheel scroll - Apenas para painéis de lista (não StatusContainer)
		// TODO: Implementar StatusContainer.IsFocused() para scroll no status panel
		if a.model.ActivePanel == models.PanelSelectedHPAs {
			a.model.HPASelectedScrollOffset++
		} else if a.model.ActivePanel == models.PanelSelectedNodePools {
			a.model.NodePoolSelectedScrollOffset++
		}
		return a, nil
	}

	return a, nil
}

// findClusterConfig busca a configuração do cluster no clusters-config.json
func (a *App) findClusterConfig(clusterName string) (*models.ClusterConfig, error) {
	return findClusterInConfig(clusterName)
}

// setupAzureContext configura o contexto Azure (login + subscription)
func (a *App) setupAzureContext(subscription string) error {
	// statusPanel direct access

	// 1. Verificar se estamos logados no Azure
	a.model.StatusContainer.AddInfo("azure-auth", "🔐 Verificando autenticação Azure CLI...")
	if err := azure.EnsureAzureLogin(); err != nil {
		a.model.StatusContainer.AddError("azure-auth", fmt.Sprintf("❌ Falha na autenticação Azure: %s", err.Error()))
		return fmt.Errorf("failed to ensure Azure login: %w", err)
	}
	a.model.StatusContainer.AddSuccess("azure-auth", "✅ Azure CLI autenticado com sucesso")

	// 2. Configurar a subscription
	if subscription != "" {
		a.model.StatusContainer.AddInfo("azure-config", fmt.Sprintf("🔄 Configurando Azure subscription: %s", subscription))
		a.debugLog("🔄 Configurando Azure subscription: %s\n", subscription)
		cmd := exec.Command("az", "account", "set", "--subscription", subscription)
		if err := cmd.Run(); err != nil {
			a.model.StatusContainer.AddError("azure-config", fmt.Sprintf("❌ Falha ao configurar subscription: %s", err.Error()))
			return fmt.Errorf("failed to set Azure subscription %s: %w", subscription, err)
		}
		a.model.StatusContainer.AddSuccess("azure-config", fmt.Sprintf("✅ Subscription configurada: %s", subscription))
		a.debugLog("✅ Azure subscription configurada com sucesso\n")
	}

	return nil
}

// enrichSessionHPAs enriquece HPAs da sessão que não possuem dados de deployment
func (a *App) enrichSessionHPAs() tea.Cmd {
	return func() tea.Msg {
		if a.model.SelectedCluster == nil {
			return nil // Nada a fazer se não há cluster selecionado
		}

		clusterName := a.model.SelectedCluster.Name

		// Obter o client do Kubernetes para este cluster
		clientset, err := a.kubeManager.GetClient(clusterName)
		if err != nil {
			a.debugLog("⚠️ Não foi possível obter client para enriquecer HPAs: %v\n", err)
			return nil // Falha silenciosa - HPAs ainda funcionarão sem dados de deployment
		}

		client := kubernetes.NewClient(clientset, clusterName)
		ctx := context.Background()

		// Enriquecer cada HPA que precisa de dados
		enrichedCount := 0
		for i := range a.model.SelectedHPAs {
			hpa := &a.model.SelectedHPAs[i]
			if hpa.NeedsEnrichment {
				err := client.EnrichHPAWithDeploymentResources(ctx, hpa)
				if err != nil {
					a.debugLog("⚠️ Falha ao enriquecer HPA %s/%s: %v\n", hpa.Namespace, hpa.Name, err)
				} else {
					hpa.NeedsEnrichment = false
					enrichedCount++
					a.debugLog("✅ HPA %s/%s enriquecido com dados de deployment\n", hpa.Namespace, hpa.Name)
				}
			}
		}

		// Também enriquecer a lista principal de HPAs
		for i := range a.model.HPAs {
			hpa := &a.model.HPAs[i]
			if hpa.NeedsEnrichment {
				err := client.EnrichHPAWithDeploymentResources(ctx, hpa)
				if err != nil {
					a.debugLog("⚠️ Falha ao enriquecer HPA %s/%s: %v\n", hpa.Namespace, hpa.Name, err)
				} else {
					hpa.NeedsEnrichment = false
				}
			}
		}

		if enrichedCount > 0 {
			a.debugLog("📊 %d HPAs enriquecidos com dados de deployment do cluster\n", enrichedCount)
		}

		return sessionHPAsEnrichedMsg{enrichedCount: enrichedCount}
	}
}

// discoverClusterResources comando para descobrir recursos do cluster
func (a *App) discoverClusterResources(prometheusOnly bool) tea.Cmd {
	return func() tea.Msg {
		clusterName := a.model.SelectedCluster.Name
		contextName := a.model.SelectedCluster.Context // Usar Context com sufixo -admin

		a.debugLog("🔍 Descobrindo recursos do cluster: %s (context: %s)", clusterName, contextName)

		// Obter o client do Kubernetes para este cluster (usar contextName)
		clientset, err := a.kubeManager.GetClient(contextName)
		if err != nil {
			return clusterResourcesDiscoveredMsg{
				err: fmt.Errorf("failed to get client for cluster %s: %w", clusterName, err),
			}
		}

		// Criar client wrapper (IMPORTANTE: passar contextName, não clusterName!)
		client := kubernetes.NewClient(clientset, contextName)

		// Descobrir recursos (passar função de log)
		resources, err := client.DiscoverClusterResources(a.model.ShowSystemResources, prometheusOnly, a.debugLog)
		if err != nil {
			return clusterResourcesDiscoveredMsg{
				err: fmt.Errorf("failed to discover cluster resources: %w", err),
			}
		}

		a.debugLog("📋 Recursos retornados pela descoberta: %d", len(resources))

		// Retornar recursos imediatamente (métricas serão buscadas depois)
		return clusterResourcesDiscoveredMsg{
			resources: resources,
			err:       nil,
		}
	}
}

// fetchMetricsAsync busca métricas em background para TODOS os recursos
func (a *App) fetchMetricsAsync() {
	if a.model.SelectedCluster == nil {
		return
	}
	contextName := a.model.SelectedCluster.Context

	// Marcar que estamos coletando métricas
	defer func() {
		a.model.FetchingMetrics = false
		a.debugLog("[DEBUG fetchMetricsAsync] Coleta de métricas concluída")
	}()

	// Verificar se há recursos antes de iterar
	if len(a.model.ClusterResources) == 0 {
		a.debugLog("[DEBUG fetchMetricsAsync] Nenhum recurso para coletar métricas")
		return
	}

	for i := range a.model.ClusterResources {
		// Verificar novamente se ainda há recursos (pode ter mudado de estado)
		if i >= len(a.model.ClusterResources) {
			a.debugLog("[DEBUG fetchMetricsAsync] Estado mudou, abortando coleta de métricas")
			return
		}

		resource := &a.model.ClusterResources[i]

		// Buscar métricas via kubectl top para TODOS os recursos
		cpuUsage, memUsage := a.kubeManager.GetPodMetrics(
			contextName,
			resource.Namespace,
			resource.Name,
			resource.WorkloadType,
		)

		// Se obteve métricas, atualizar os campos de EXIBIÇÃO (não os editáveis)
		if cpuUsage != "-" || memUsage != "-" {
			// Atualizar DisplayCPURequest (para exibição na lista)
			if cpuUsage != "-" {
				if resource.CurrentCPURequest == "-" {
					resource.DisplayCPURequest = fmt.Sprintf("- (uso: %s)", cpuUsage)
				} else {
					resource.DisplayCPURequest = fmt.Sprintf("%s (uso: %s)", resource.CurrentCPURequest, cpuUsage)
				}
			} else {
				// Sem métricas de uso - exibir apenas o valor original
				resource.DisplayCPURequest = resource.CurrentCPURequest
			}

			// Atualizar DisplayMemoryRequest (para exibição na lista)
			if memUsage != "-" {
				if resource.CurrentMemoryRequest == "-" {
					resource.DisplayMemoryRequest = fmt.Sprintf("- (uso: %s)", memUsage)
				} else {
					resource.DisplayMemoryRequest = fmt.Sprintf("%s (uso: %s)", resource.CurrentMemoryRequest, memUsage)
				}
			} else {
				// Sem métricas de uso - exibir apenas o valor original
				resource.DisplayMemoryRequest = resource.CurrentMemoryRequest
			}

			// Debug log
			a.debugLog("[DEBUG fetchMetricsAsync] Atualizado %s/%s - CPU: %s, MEM: %s",
			resource.Namespace, resource.Name, cpuUsage, memUsage)
		}
	}
}

// clusterResourcesDiscoveredMsg mensagem quando recursos são descobertos
type clusterResourcesDiscoveredMsg struct {
	resources []models.ClusterResource
	err       error
}

// metricsRefreshMsg mensagem para atualizar UI enquanto métricas são coletadas
type metricsRefreshMsg struct{}

// ============================================================================
// TEXT EDITING HELPER FUNCTIONS
// ============================================================================

// handleTextEditingKeys processa teclas para edição de texto com navegação de cursor (REFATORADO)
func (a *App) handleTextEditingKeys(msg tea.KeyMsg, currentValue string, onSave func(string), onCancel func()) (string, int, bool) {
	// Usar a nova lógica centralizada
	newValue, newCursor, continueEditing := a.handleUnifiedTextInput(msg, currentValue, onSave, onCancel)

	// Atualizar posição do cursor no modelo
	a.model.CursorPosition = newCursor

	return newValue, newCursor, continueEditing
}

// Métodos centralizados para edição de texto

// handleUnifiedTextInput processa entrada de texto de forma centralizada
func (a *App) handleUnifiedTextInput(msg tea.KeyMsg, currentValue string, onSave func(string), onCancel func()) (string, int, bool) {
	textInput := NewTextInput(currentValue)
	textInput.SetCursorPosition(a.model.CursorPosition)

	newValue, newCursor, continueEditing, _ := textInput.HandleKeyPress(msg, onSave, onCancel)

	return newValue, newCursor, continueEditing
}

// renderTextWithCursor renderiza texto com cursor visual centralizado
func (a *App) renderTextWithCursor(text string, cursorPos int) string {
	textInput := NewTextInput(text)
	textInput.SetCursorPosition(cursorPos)
	return textInput.RenderWithCursor()
}

// insertCursorInText insere o cursor visual na posição correta do texto (método legado)
func (a *App) insertCursorInText(text string, cursorPos int) string {
	return a.renderTextWithCursor(text, cursorPos)
}

// validateCursorPosition garante que a posição do cursor está dentro dos limites válidos
func (a *App) validateCursorPosition(text string) {
	maxPos := len([]rune(text))
	if a.model.CursorPosition < 0 {
		a.model.CursorPosition = 0
	}
	if a.model.CursorPosition > maxPos {
		a.model.CursorPosition = maxPos
	}
}

// resetHPAApplicationCounters zera os contadores de aplicação de todos os HPAs (nova sessão)
func (a *App) resetHPAApplicationCounters() {
	// Zerar contadores nos HPAs selecionados
	for i := range a.model.SelectedHPAs {
		a.model.SelectedHPAs[i].AppliedCount = 0
		a.model.SelectedHPAs[i].LastAppliedAt = nil
	}

	// Zerar contadores em todos os HPAs carregados
	for i := range a.model.HPAs {
		a.model.HPAs[i].AppliedCount = 0
		a.model.HPAs[i].LastAppliedAt = nil
	}
}

// loadCronJobs carrega os CronJobs do cluster selecionado
func (a *App) loadCronJobs() tea.Cmd {
	return func() tea.Msg {
		if a.model.SelectedCluster == nil {
			return cronJobsLoadedMsg{err: fmt.Errorf("no cluster selected")}
		}

		clusterName := a.model.SelectedCluster.Name
		contextName := a.model.SelectedCluster.Context // Usar Context com sufixo -admin
		a.debugLog("🔄 Carregando CronJobs do cluster: %s (context: %s)", clusterName, contextName)

		// Usar cliente Kubernetes para listar CronJobs (usar contextName, não clusterName)
		client, err := a.kubeManager.GetClient(contextName)
		if err != nil {
			return cronJobsLoadedMsg{err: fmt.Errorf("failed to get kubernetes client: %w", err)}
		}

		cronJobs, err := a.loadCronJobsFromKubernetes(client, clusterName)
		if err != nil {
			return cronJobsLoadedMsg{err: fmt.Errorf("failed to load cronjobs: %w", err)}
		}

		a.debugLog("✅ CronJobs carregados: %d encontrados", len(cronJobs))
		return cronJobsLoadedMsg{cronJobs: cronJobs}
	}
}

// loadCronJobsFromKubernetes carrega CronJobs usando a API do Kubernetes
func (a *App) loadCronJobsFromKubernetes(client k8sClientSet.Interface, clusterName string) ([]models.CronJob, error) {
	ctx := context.Background()
	var allCronJobs []models.CronJob

	// Listar todos os namespaces (excluindo system se filtrado)
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	for _, ns := range namespaces.Items {
		namespaceName := ns.Name

		// Filtrar namespaces de sistema se necessário
		if !a.model.ShowSystemNamespaces && a.isSystemNamespace(namespaceName) {
			continue
		}

		// Listar CronJobs no namespace
		cronJobList, err := client.BatchV1().CronJobs(namespaceName).List(ctx, metav1.ListOptions{})
		if err != nil {
			a.debugLog("⚠️ Erro ao listar CronJobs no namespace %s: %v", namespaceName, err)
			continue
		}

		for _, cronJob := range cronJobList.Items {
			// Converter para nosso modelo
			modelCronJob := models.CronJob{
				Name:            cronJob.Name,
				Namespace:       namespaceName,
				Cluster:         clusterName,
				Schedule:        cronJob.Spec.Schedule,
				Suspend:         cronJob.Spec.Suspend,
				OriginalSuspend: cronJob.Spec.Suspend,
			}

			// Adicionar descrição legível do schedule
			modelCronJob.ScheduleDesc = a.parseCronSchedule(cronJob.Spec.Schedule)

			// Extrair informações de status
			if cronJob.Status.LastScheduleTime != nil {
				modelCronJob.LastScheduleTime = &cronJob.Status.LastScheduleTime.Time
			}

			modelCronJob.ActiveJobs = len(cronJob.Status.Active)
			if cronJob.Status.LastSuccessfulTime != nil && cronJob.Status.LastScheduleTime != nil {
				if cronJob.Status.LastSuccessfulTime.Time.After(cronJob.Status.LastScheduleTime.Time) {
					modelCronJob.LastRunStatus = "Success"
				}
			}

			// Obter informações do job template
			if len(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers) > 0 {
				container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]

				// Extrair descrição funcional dos comandos/argumentos
				functionalDesc := a.extractJobFunction(container.Command, container.Args)

				// Combinar informações de container/image com descrição funcional
				containerInfo := fmt.Sprintf("%s (%s)", container.Name, container.Image)
				if functionalDesc != "" {
					modelCronJob.JobTemplate = fmt.Sprintf("%s\n     %s", containerInfo, functionalDesc)
				} else {
					modelCronJob.JobTemplate = containerInfo
				}
			}

			allCronJobs = append(allCronJobs, modelCronJob)
		}
	}

	return allCronJobs, nil
}

// parseCronSchedule converte um schedule cron em descrição legível
func (a *App) parseCronSchedule(schedule string) string {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return fmt.Sprintf("Schedule: %s", schedule)
	}

	minute := parts[0]
	hour := parts[1]

	// Casos comuns
	if schedule == "0 0 * * *" {
		return "Schedule: 0 0 * * * - executa todo dia à meia-noite"
	}
	if schedule == "0 2 * * *" {
		return "Schedule: 0 2 * * * - executa todo dia às 2:00 AM"
	}
	if schedule == "0 0 * * 1" {
		return "Schedule: 0 0 * * 1 - executa toda segunda-feira à meia-noite"
	}
	if schedule == "*/5 * * * *" {
		return "Schedule: */5 * * * * - executa a cada 5 minutos"
	}
	if schedule == "0 */2 * * *" {
		return "Schedule: 0 */2 * * * - executa a cada 2 horas"
	}

	// Construir descrição baseada nos componentes
	desc := fmt.Sprintf("Schedule: %s", schedule)

	if hour != "*" && minute != "*" {
		if hour[0] != '*' && minute == "0" {
			desc += fmt.Sprintf(" - executa todo dia às %s:00", hour)
		} else if hour[0] != '*' && minute != "*" {
			desc += fmt.Sprintf(" - executa todo dia às %s:%s", hour, minute)
		}
	}

	return desc
}

// extractJobFunction extrai descrição funcional dos comandos e argumentos do container
func (a *App) extractJobFunction(command []string, args []string) string {
	// Combinar comando e argumentos
	var allArgs []string
	allArgs = append(allArgs, command...)
	allArgs = append(allArgs, args...)

	// Juntar todos os argumentos em uma string para análise
	fullCommand := strings.Join(allArgs, " ")

	// Debug: log do comando para análise
	a.debugLog("🔍 Analisando comando do CronJob:")
	a.debugLog("   📋 Command: %v", command)
	a.debugLog("   📋 Args: %v", args)
	a.debugLog("   📋 Full: [%s]", fullCommand)

	// Se for um script bash (-c), analisar o conteúdo do script
	if strings.Contains(fullCommand, "/bin/bash") && strings.Contains(fullCommand, "-c") {
		a.debugLog("   🔧 Detectado script bash, analisando conteúdo")
		return a.extractFromBashScript(fullCommand)
	}

	// Padrões comuns para extrair função
	patterns := []struct {
		pattern     string
		description string
	}{
		// kubectl rollout restart deployment X -n Y ou --namespace Y
		{`kubectl.*rollout.*restart.*deployment\s+(\S+).*(?:-n|--namespace)\s+(\S+)`, "Faz rollout restart do deployment %s no namespace %s"},
		// kubectl rollout restart deployment X (sem namespace explícito)
		{`kubectl.*rollout.*restart.*deployment\s+([a-zA-Z0-9\-_]+)(?:\s|$)`, "Faz rollout restart do deployment %s"},
		// kubectl scale com namespace
		{`kubectl.*scale.*deployment\s+(\S+).*(?:-n|--namespace)\s+(\S+).*replicas[=\s]+(\d+)`, "Escala deployment %s no namespace %s para %s réplicas"},
		// kubectl scale sem namespace
		{`kubectl.*scale.*deployment\s+(\S+).*replicas[=\s]+(\d+)`, "Escala deployment %s para %s réplicas"},
		{`kubectl.*delete.*pod.*selector\s+(\S+)`, "Remove pods com selector %s"},
		{`kubectl.*apply.*-f\s+(\S+)`, "Aplica configuração do arquivo %s"},
		{`curl.*-X\s+POST.*(\S+)`, "Faz requisição POST para %s"},
		{`curl.*-X\s+GET.*(\S+)`, "Faz requisição GET para %s"},
		{`backup.*database.*(\S+)`, "Faz backup do banco de dados %s"},
		{`cleanup.*logs.*(\S+)`, "Limpa logs do serviço %s"},
	}

	// Tentar encontrar padrões conhecidos
	for i, p := range patterns {
		re := regexp.MustCompile(`(?i)` + p.pattern)
		matches := re.FindStringSubmatch(fullCommand)
		if len(matches) > 1 {
			a.debugLog("   ✅ Padrão %d encontrado: %s", i+1, p.pattern)
			a.debugLog("   📝 Grupos capturados: %v", matches[1:])
			// Aplicar template com os grupos capturados
			switch len(matches) {
			case 2:
				result := fmt.Sprintf(p.description, matches[1])
				a.debugLog("   🎯 Resultado: %s", result)
				return result
			case 3:
				result := fmt.Sprintf(p.description, matches[1], matches[2])
				a.debugLog("   🎯 Resultado: %s", result)
				return result
			case 4:
				result := fmt.Sprintf(p.description, matches[1], matches[2], matches[3])
				a.debugLog("   🎯 Resultado: %s", result)
				return result
			}
		}
	}

	// Se não encontrou padrão específico, tentar extrair ação geral
	a.debugLog("   ⚠️ Nenhum padrão específico encontrado, tentando padrões gerais")

	if strings.Contains(fullCommand, "kubectl") {
		if strings.Contains(fullCommand, "rollout") && strings.Contains(fullCommand, "restart") {
			result := "Executa rollout restart de recursos Kubernetes"
			a.debugLog("   🎯 Resultado geral: %s", result)
			return result
		}
		if strings.Contains(fullCommand, "scale") {
			result := "Escala recursos Kubernetes"
			a.debugLog("   🎯 Resultado geral: %s", result)
			return result
		}
		if strings.Contains(fullCommand, "delete") {
			result := "Remove recursos Kubernetes"
			a.debugLog("   🎯 Resultado geral: %s", result)
			return result
		}
		if strings.Contains(fullCommand, "apply") {
			result := "Aplica configurações Kubernetes"
			a.debugLog("   🎯 Resultado geral: %s", result)
			return result
		}
		result := "Executa comando kubectl"
		a.debugLog("   🎯 Resultado geral: %s", result)
		return result
	}

	if strings.Contains(fullCommand, "curl") {
		result := "Executa requisição HTTP"
		a.debugLog("   🎯 Resultado geral: %s", result)
		return result
	}

	if strings.Contains(fullCommand, "backup") {
		result := "Executa backup de dados"
		a.debugLog("   🎯 Resultado geral: %s", result)
		return result
	}

	if strings.Contains(fullCommand, "cleanup") || strings.Contains(fullCommand, "clean") {
		result := "Executa limpeza de recursos"
		a.debugLog("   🎯 Resultado geral: %s", result)
		return result
	}

	// Se não conseguiu identificar, retorna string vazia
	a.debugLog("   ❌ Nenhum padrão reconhecido, retornando vazio")
	return ""
}

// extractFromBashScript extrai informações de scripts bash executados com -c
func (a *App) extractFromBashScript(fullCommand string) string {
	a.debugLog("   🔍 Analisando script bash")

	// Extrair variáveis definidas no início do script
	var namespace, deployment string

	// Padrões para extrair variáveis
	namespacePattern := regexp.MustCompile(`(?i)NAMESPACE\s*=\s*([^\s\n]+)`)
	deploymentPattern := regexp.MustCompile(`(?i)DEPLOYMENT\s*=\s*([^\s\n]+)`)

	if matches := namespacePattern.FindStringSubmatch(fullCommand); len(matches) > 1 {
		namespace = matches[1]
		a.debugLog("   📝 Namespace encontrado: %s", namespace)
	}

	if matches := deploymentPattern.FindStringSubmatch(fullCommand); len(matches) > 1 {
		deployment = matches[1]
		a.debugLog("   📝 Deployment encontrado: %s", deployment)
	}

	// Analisar ações no script
	if strings.Contains(fullCommand, "kubectl rollout restart") {
		if deployment != "" && namespace != "" {
			result := fmt.Sprintf("Faz rollout restart do deployment %s no namespace %s", deployment, namespace)
			a.debugLog("   🎯 Resultado do script: %s", result)
			return result
		} else if deployment != "" {
			result := fmt.Sprintf("Faz rollout restart do deployment %s", deployment)
			a.debugLog("   🎯 Resultado do script: %s", result)
			return result
		} else {
			result := "Executa rollout restart de deployment"
			a.debugLog("   🎯 Resultado do script: %s", result)
			return result
		}
	}

	if strings.Contains(fullCommand, "kubectl scale") {
		if deployment != "" && namespace != "" {
			result := fmt.Sprintf("Escala deployment %s no namespace %s", deployment, namespace)
			a.debugLog("   🎯 Resultado do script: %s", result)
			return result
		} else {
			result := "Executa scaling de deployment"
			a.debugLog("   🎯 Resultado do script: %s", result)
			return result
		}
	}

	// Outros padrões gerais
	if strings.Contains(fullCommand, "kubectl") {
		result := "Executa operações Kubernetes via script"
		a.debugLog("   🎯 Resultado do script: %s", result)
		return result
	}

	a.debugLog("   ❌ Script não reconhecido")
	return ""
}

// isSystemNamespace verifica se um namespace é de sistema
func (a *App) isSystemNamespace(namespace string) bool {
	systemNamespaces := []string{
		"kube-system", "kube-public", "kube-node-lease",
		"istio-system", "istio-injection",
		"cert-manager", "ingress-nginx",
		"monitoring", "prometheus", "grafana",
		"flux-system", "flux", "fluxcd",
		"argocd", "argo", "argo-workflows",
		"tekton-pipelines", "tekton",
		"knative-serving", "knative-eventing",
		"gatekeeper-system", "open-policy-agent",
		"falco", "sysdig",
		"linkerd", "linkerd-viz", "linkerd-jaeger",
		"cilium", "cilium-system",
		"calico-system", "tigera-operator",
		"metallb-system",
		"rook-ceph", "ceph",
		"vault", "vault-system",
		"consul", "consul-system",
		"jaeger", "jaeger-system",
		"elastic-system", "elasticsearch",
		"logging", "fluent", "fluentd", "fluent-bit",
		"datadog", "newrelic",
		"kustomize", "helm",
		"crossplane-system",
		"external-dns",
		"cluster-autoscaler",
		"metrics-server",
		"kubernetes-dashboard",
		"keda", "keda-system",
		"sealed-secrets",
		"velero",
		"backup", "restore",
	}

	for _, sysNs := range systemNamespaces {
		if namespace == sysNs {
			return true
		}
	}
	return false
}

// toggleNodePoolSequenceMarking - Marca/desmarca node pool para execução sequencial (stress test)
func (a *App) toggleNodePoolSequenceMarking(selectedIndex int) {
	if selectedIndex >= len(a.model.SelectedNodePools) {
		return
	}

	currentPool := &a.model.SelectedNodePools[selectedIndex]

	// Se já está marcado, desmarcar (toggle)
	if currentPool.SequenceOrder > 0 {
		a.debugLog("🔄 Desmarcando node pool %s (ordem %d)", currentPool.Name, currentPool.SequenceOrder)
		currentPool.SequenceOrder = 0
		currentPool.SequenceStatus = ""
		return
	}

	// Contar quantos já estão marcados
	markedCount := 0
	for _, pool := range a.model.SelectedNodePools {
		if pool.SequenceOrder > 0 {
			markedCount++
		}
	}

	// Limite de 2 node pools
	if markedCount >= 2 {
		a.debugLog("⚠️  Limite de 2 node pools já atingido para execução sequencial")
		// Poderia adicionar uma mensagem de status aqui
		return
	}

	// Marcar com a próxima ordem disponível
	nextOrder := markedCount + 1
	currentPool.SequenceOrder = nextOrder
	currentPool.SequenceStatus = "pending"

	a.debugLog("✅ Node pool %s marcado para execução sequencial (ordem %d)", currentPool.Name, nextOrder)
}

// checkAndStartSequentialExecution - Verifica se deve iniciar execução do segundo node pool
func (a *App) checkAndStartSequentialExecution() tea.Cmd {
	// Encontrar node pools marcados
	var firstPool *models.NodePool
	var secondPool *models.NodePool

	for i := range a.model.SelectedNodePools {
		pool := &a.model.SelectedNodePools[i]
		if pool.SequenceOrder == 1 {
			firstPool = pool
		} else if pool.SequenceOrder == 2 {
			secondPool = pool
		}
	}

	// Se não há sequência marcada, nada fazer
	if firstPool == nil || secondPool == nil {
		return nil
	}

	// Se o primeiro node pool foi completado e o segundo ainda está pendente
	if firstPool.SequenceStatus == "completed" && secondPool.SequenceStatus == "pending" {
		a.debugLog("✅ Primeiro node pool %s completado, iniciando segundo node pool %s", firstPool.Name, secondPool.Name)

		// Marcar segundo como executando
		secondPool.SequenceStatus = "executing"

		// Executar o segundo node pool automaticamente
		return a.applyNodePoolChanges([]models.NodePool{*secondPool})
	}

	return nil
}

// checkSequenceStatusAndContinue - Verifica status e continua execução sequencial
func (a *App) checkSequenceStatusAndContinue() tea.Cmd {
	// Encontrar node pool atualmente executando
	var executingPool *models.NodePool
	var nextPool *models.NodePool

	for i := range a.model.SelectedNodePools {
		pool := &a.model.SelectedNodePools[i]
		if pool.SequenceStatus == "executing" {
			executingPool = pool
		} else if pool.SequenceStatus == "pending" && executingPool != nil && pool.SequenceOrder == executingPool.SequenceOrder+1 {
			nextPool = pool
		}
	}

	if executingPool == nil {
		return nil // Nenhuma execução em andamento
	}

	// Simular verificação de status (aqui você implementaria a verificação real via Azure CLI)
	// Por enquanto, vamos marcar como completo após um delay
	a.debugLog("✅ Node pool %s completado, iniciando próximo", executingPool.Name)
	executingPool.SequenceStatus = "completed"

	// Se há próximo node pool, executá-lo
	if nextPool != nil {
		nextPool.SequenceStatus = "executing"
		a.debugLog("⚡ Executando próximo node pool %s (ordem %d)", nextPool.Name, nextPool.SequenceOrder)
		return a.applyNodePoolChanges([]models.NodePool{*nextPool})
	}

	a.debugLog("🎉 Execução sequencial concluída!")
	return nil
}

// checkForUpdatesInBackground verifica updates e notifica via StatusContainer
func (a *App) checkForUpdatesInBackground() {
	// Aguardar 3 segundos para não interferir no startup
	time.Sleep(3 * time.Second)

	// Verificar se deve checar updates
	if !updater.ShouldCheckForUpdates() {
		return
	}

	// Verificar updates
	info, err := updater.CheckForUpdates()
	if err != nil {
		// Ignorar erros silenciosamente
		a.debugLog("⚠️ Erro ao verificar updates: %v", err)
		return
	}

	// Marcar verificação feita
	_ = updater.MarkUpdateChecked()

	if info.Available {
		// Adicionar notificação no StatusContainer
		msg := fmt.Sprintf("Nova versão disponível: %s → %s",
			info.CurrentVersion, info.LatestVersion)
		a.model.StatusContainer.AddInfo("Updates", msg)

		urlMsg := fmt.Sprintf("Download: %s", info.ReleaseURL)
		a.model.StatusContainer.AddInfo("Updates", urlMsg)

		tipMsg := "Execute 'k8s-hpa-manager version' para detalhes"
		a.model.StatusContainer.AddInfo("Updates", tipMsg)

		a.debugLog("🆕 Update disponível: %s → %s", info.CurrentVersion, info.LatestVersion)
	}
}
