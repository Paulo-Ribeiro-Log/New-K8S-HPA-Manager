package models

import (
	"fmt"
	"strings"
	"time"
)

// StatusContainerInterface define o contrato para containers de status
type StatusContainerInterface interface {
	AddSuccess(source, content string)
	AddError(source, content string)
	AddWarning(source, content string)
	AddInfo(source, content string)
	AddDebug(source, content string)
	Clear()
	Render() string
	ScrollUp()
	ScrollDown()
	// Progress bar methods
	AddProgressBar(id, title string, total int)
	UpdateProgress(id string, percentage int, statusText string)
	CompleteProgress(id string)
	RemoveProgress(id string)
	ClearProgressBars()
}

// Tab representa uma aba individual com seu próprio contexto
type Tab struct {
	ID              string       // ID único da aba
	Name            string       // Nome exibido na aba (ex: "akspriv-prod")
	ClusterContext  string       // Contexto do cluster Kubernetes
	Model           *AppModel    // Estado completo da aba (isolado)
	Active          bool         // Se é a aba ativa
	CreatedAt       time.Time    // Quando foi criada
	LastAccessedAt  time.Time    // Último acesso
	Modified        bool         // Se tem modificações não salvas
}

// TabManager gerencia múltiplas abas
type TabManager struct {
	Tabs      []*Tab  // Lista de abas (máximo 10)
	ActiveIdx int     // Índice da aba ativa (0-9)
	MaxTabs   int     // Máximo de abas permitidas (10)
}

// NewTabManager cria um novo gerenciador de abas
func NewTabManager() *TabManager {
	return &TabManager{
		Tabs:      make([]*Tab, 0),
		ActiveIdx: 0,
		MaxTabs:   10, // Alt+1 até Alt+9, Alt+0 para a 10ª
	}
}

// AddTab adiciona uma nova aba
func (tm *TabManager) AddTab(name, clusterContext string, model *AppModel) bool {
	if len(tm.Tabs) >= tm.MaxTabs {
		return false // Limite atingido
	}

	tab := &Tab{
		ID:             fmt.Sprintf("tab-%d-%d", time.Now().Unix(), len(tm.Tabs)),
		Name:           name,
		ClusterContext: clusterContext,
		Model:          model,
		Active:         false,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
		Modified:       false,
	}

	tm.Tabs = append(tm.Tabs, tab)
	tm.SwitchToTab(len(tm.Tabs) - 1) // Ativa a nova aba
	return true
}

// SwitchToTab muda para a aba especificada
func (tm *TabManager) SwitchToTab(index int) bool {
	if index < 0 || index >= len(tm.Tabs) {
		return false
	}

	// Desativa a aba atual
	if tm.ActiveIdx >= 0 && tm.ActiveIdx < len(tm.Tabs) {
		tm.Tabs[tm.ActiveIdx].Active = false
	}

	// Ativa a nova aba
	tm.ActiveIdx = index
	tm.Tabs[index].Active = true
	tm.Tabs[index].LastAccessedAt = time.Now()
	return true
}

// GetActiveTab retorna a aba ativa
func (tm *TabManager) GetActiveTab() *Tab {
	if tm.ActiveIdx >= 0 && tm.ActiveIdx < len(tm.Tabs) {
		return tm.Tabs[tm.ActiveIdx]
	}
	return nil
}

// CloseTab fecha a aba especificada
func (tm *TabManager) CloseTab(index int) bool {
	if index < 0 || index >= len(tm.Tabs) || len(tm.Tabs) == 1 {
		return false // Não pode fechar a última aba
	}

	// Remove a aba
	tm.Tabs = append(tm.Tabs[:index], tm.Tabs[index+1:]...)

	// Ajusta o índice ativo
	if tm.ActiveIdx >= len(tm.Tabs) {
		tm.ActiveIdx = len(tm.Tabs) - 1
	}

	// Ativa a aba ajustada
	if tm.ActiveIdx >= 0 && tm.ActiveIdx < len(tm.Tabs) {
		tm.Tabs[tm.ActiveIdx].Active = true
	}

	return true
}

// GetTabCount retorna o número de abas
func (tm *TabManager) GetTabCount() int {
	return len(tm.Tabs)
}

// CanAddTab verifica se pode adicionar mais abas
func (tm *TabManager) CanAddTab() bool {
	return len(tm.Tabs) < tm.MaxTabs
}

// AppState representa o estado principal da aplicação
type AppState int

const (
	StateClusterSelection AppState = iota
	StateSessionFolderSelection  // Seleção de pasta para sessões
	StateSessionSelection
	StateSessionFolderSave      // Seleção de pasta para salvar
	StateNamespaceSelection
	StateHPASelection
	StateHPAEditing
	StateNodeSelection
	StateNodeEditing
	StateMixedSession  // Novo estado para sessões mistas
	StateClusterResourceDiscovery  // F7/F8 - Loading resources
	StateClusterResourceSelection  // Selecionando recursos
	StateClusterResourceEditing    // Editando recurso específico
	StatePrometheusStackManagement // F8 - Gestão específica Prometheus
	StateCronJobSelection          // F9 - Seleção de CronJobs
	StateCronJobEditing           // Editando CronJob específico
	StateAddingCluster            // F7 - Adicionando novo cluster
	StateLogViewer                // F3 - Visualização de logs
	StateHelp
)

// Campos editáveis de HPA
const (
	FieldMinReplicas         = "min_replicas"
	FieldMaxReplicas         = "max_replicas"
	FieldTargetCPU           = "target_cpu"
	FieldTargetMemory        = "target_memory"
	FieldRollout             = "rollout"
	FieldDaemonSetRollout    = "daemonset_rollout"
	FieldStatefulSetRollout  = "statefulset_rollout"
	FieldDeploymentCPUReq    = "deployment_cpu_request"
	FieldDeploymentCPULimit  = "deployment_cpu_limit"
	FieldDeploymentMemReq    = "deployment_memory_request"
	FieldDeploymentMemLimit  = "deployment_memory_limit"
)

// Campos editáveis de Node Pool
const (
	FieldNodeCount    = "node_count"
	FieldMinNodes     = "min_nodes"
	FieldMaxNodes     = "max_nodes"
	FieldAutoscaling  = "autoscaling_enabled"
)

// Campos editáveis de Cluster Resources
const (
	FieldCPURequest    = "cpu_request"
	FieldCPULimit      = "cpu_limit"
	FieldMemoryRequest = "memory_request"
	FieldMemoryLimit   = "memory_limit"
	FieldReplicas      = "replicas"
	FieldStorageSize   = "storage_size"
)


// GlobalPositionMemory gerencia a memorização de posições globalmente
type GlobalPositionMemory struct {
	PreviousState     *PanelState `json:"previous_state"`      // Estado anterior para ESC
	TabMemory         *PanelState `json:"tab_memory"`          // Memorização específica do TAB
	SpaceMemory       *PanelState `json:"space_memory"`        // Memorização específica do SPACE
	EnterMemory       *PanelState `json:"enter_memory"`        // Memorização específica do ENTER
	LastAction        string      `json:"last_action"`         // Última ação executada
	LastActionTime    time.Time   `json:"last_action_time"`    // Quando foi a última ação
}

// Cluster representa um cluster Kubernetes
type Cluster struct {
	Name     string           `json:"name"`
	Context  string           `json:"context"`
	Status   ConnectionStatus `json:"status"`
	Error    string           `json:"error,omitempty"`
	Selected bool             `json:"selected"`
}

// ConnectionStatus indica o status de conectividade do cluster
type ConnectionStatus int

const (
	StatusUnknown ConnectionStatus = iota
	StatusConnected
	StatusTimeout
	StatusError
)

func (s ConnectionStatus) String() string {
	switch s {
	case StatusConnected:
		return "connected ✓"
	case StatusTimeout:
		return "timeout ⚠️"
	case StatusError:
		return "error ❌"
	default:
		return "checking..."
	}
}

// Namespace representa um namespace Kubernetes
type Namespace struct {
	Name     string `json:"name"`
	Cluster  string `json:"cluster"`
	HPACount int    `json:"hpa_count"`
	Selected bool   `json:"selected"`
}

// HPA representa um Horizontal Pod Autoscaler
type HPA struct {
	Name            string     `json:"name"`
	Namespace       string     `json:"namespace"`
	Cluster         string     `json:"cluster"`
	MinReplicas     *int32     `json:"min_replicas"`
	MaxReplicas     int32      `json:"max_replicas"`
	CurrentReplicas int32      `json:"current_replicas"`
	TargetCPU       *int32     `json:"target_cpu,omitempty"`
	TargetMemory    *int32     `json:"target_memory,omitempty"`
	PerformRollout  bool       `json:"perform_rollout"`
	PerformDaemonSetRollout  bool       `json:"perform_daemonset_rollout"`
	PerformStatefulSetRollout bool      `json:"perform_statefulset_rollout"`
	Selected        bool       `json:"selected"`
	Modified        bool       `json:"modified"`
	OriginalValues  *HPAValues `json:"original_values"`
	LastUpdated     time.Time  `json:"last_updated"`

	// Application Tracking (not saved in session JSON)
	AppliedCount    int        `json:"-"` // Contador de aplicações na sessão atual
	LastAppliedAt   *time.Time `json:"-"` // Última vez que foi aplicado na sessão

	// Deployment Resource Information
	DeploymentName          string `json:"deployment_name,omitempty"`
	CurrentCPURequest       string `json:"current_cpu_request,omitempty"`
	CurrentCPULimit         string `json:"current_cpu_limit,omitempty"`
	CurrentMemoryRequest    string `json:"current_memory_request,omitempty"`
	CurrentMemoryLimit      string `json:"current_memory_limit,omitempty"`
	TargetCPURequest        string `json:"target_cpu_request,omitempty"`
	TargetCPULimit          string `json:"target_cpu_limit,omitempty"`
	TargetMemoryRequest     string `json:"target_memory_request,omitempty"`
	TargetMemoryLimit       string `json:"target_memory_limit,omitempty"`
	ResourcesModified       bool   `json:"resources_modified"`
	NeedsEnrichment         bool   `json:"-"` // Campo interno, não salvar no JSON
}

// HPAValues armazena os valores de configuração de um HPA
type HPAValues struct {
	MinReplicas         *int32 `json:"min_replicas"`
	MaxReplicas         int32  `json:"max_replicas"`
	TargetCPU           *int32 `json:"target_cpu,omitempty"`
	TargetMemory        *int32 `json:"target_memory,omitempty"`

	// Rollout Options
	PerformRollout            bool `json:"perform_rollout"`
	PerformDaemonSetRollout   bool `json:"perform_daemonset_rollout"`
	PerformStatefulSetRollout bool `json:"perform_statefulset_rollout"`

	// Deployment Resources
	DeploymentName      string `json:"deployment_name,omitempty"`
	CPURequest          string `json:"cpu_request,omitempty"`
	CPULimit            string `json:"cpu_limit,omitempty"`
	MemoryRequest       string `json:"memory_request,omitempty"`
	MemoryLimit         string `json:"memory_limit,omitempty"`
}

// HPAChange representa uma mudança em um HPA
type HPAChange struct {
	Cluster          string     `json:"cluster"`
	Namespace        string     `json:"namespace"`
	HPAName          string     `json:"hpa_name"`
	OriginalValues   *HPAValues `json:"original_values"`
	NewValues        *HPAValues `json:"new_values"`
	Applied          bool       `json:"applied"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	RolloutTriggered bool       `json:"rollout_triggered"`
	DaemonSetRolloutTriggered  bool `json:"daemonset_rollout_triggered"`
	StatefulSetRolloutTriggered bool `json:"statefulset_rollout_triggered"`
}

// HelpStateSnapshot armazena o estado completo antes de entrar no Help
type HelpStateSnapshot struct {
	State               AppState
	SelectedIndex       int
	ActivePanel         PanelType
	CurrentNamespaceIdx int
	SelectedCluster     *Cluster
	SelectedNamespaces  []Namespace
	SelectedHPAs        []HPA
	Namespaces          []Namespace
	HPAs                []HPA
	NodePools           []NodePool
	CronJobs            []CronJob
	ClusterResources    []ClusterResource
	EditingHPA          *HPA
	EditingNodePool     *NodePool
	EditingCronJob      *CronJob
	EditingResource     *ClusterResource
	CurrentSession      *Session
}

// SaveHelpSnapshot cria um snapshot do estado atual antes de entrar no Help
func (m *AppModel) SaveHelpSnapshot() {
	m.HelpStateSnapshot = &HelpStateSnapshot{
		State:               m.State,
		SelectedIndex:       m.SelectedIndex,
		ActivePanel:         m.ActivePanel,
		CurrentNamespaceIdx: m.CurrentNamespaceIdx,
		SelectedCluster:     m.SelectedCluster,
		SelectedNamespaces:  append([]Namespace{}, m.SelectedNamespaces...),
		SelectedHPAs:        append([]HPA{}, m.SelectedHPAs...),
		Namespaces:          append([]Namespace{}, m.Namespaces...),
		HPAs:                append([]HPA{}, m.HPAs...),
		NodePools:           append([]NodePool{}, m.NodePools...),
		CronJobs:            append([]CronJob{}, m.CronJobs...),
		ClusterResources:    append([]ClusterResource{}, m.ClusterResources...),
		EditingHPA:          m.EditingHPA,
		EditingNodePool:     m.EditingNodePool,
		EditingCronJob:      m.EditingCronJob,
		EditingResource:     m.EditingResource,
		CurrentSession:      m.CurrentSession,
	}
}

// RestoreHelpSnapshot restaura o estado anterior salvo antes do Help
func (m *AppModel) RestoreHelpSnapshot() {
	if m.HelpStateSnapshot == nil {
		return
	}

	snapshot := m.HelpStateSnapshot

	m.State = snapshot.State
	m.SelectedIndex = snapshot.SelectedIndex
	m.ActivePanel = snapshot.ActivePanel
	m.CurrentNamespaceIdx = snapshot.CurrentNamespaceIdx
	m.SelectedCluster = snapshot.SelectedCluster
	m.SelectedNamespaces = snapshot.SelectedNamespaces
	m.SelectedHPAs = snapshot.SelectedHPAs
	m.Namespaces = snapshot.Namespaces
	m.HPAs = snapshot.HPAs
	m.NodePools = snapshot.NodePools
	m.CronJobs = snapshot.CronJobs
	m.ClusterResources = snapshot.ClusterResources
	m.EditingHPA = snapshot.EditingHPA
	m.EditingNodePool = snapshot.EditingNodePool
	m.EditingCronJob = snapshot.EditingCronJob
	m.EditingResource = snapshot.EditingResource
	m.CurrentSession = snapshot.CurrentSession

	// Limpar snapshot após restauração
	m.HelpStateSnapshot = nil
}

// Session representa uma sessão salva de modificações
type Session struct {
	Name         string           `json:"name"`
	CreatedAt    time.Time        `json:"created_at"`
	CreatedBy    string           `json:"created_by"`
	Description  string           `json:"description,omitempty"`
	TemplateUsed string           `json:"template_used"`
	Metadata     *SessionMetadata `json:"metadata"`
	Changes          []HPAChange          `json:"changes"`
	NodePoolChanges  []NodePoolChange     `json:"node_pool_changes"`
	ResourceChanges  []ClusterResourceChange `json:"resource_changes"`
	RollbackData     *RollbackData        `json:"rollback_data"`
}

// SessionMetadata contém metadados da sessão
type SessionMetadata struct {
	ClustersAffected []string `json:"clusters_affected"`
	NamespacesCount  int      `json:"namespaces_count"`
	HPACount         int      `json:"hpa_count"`
	NodePoolCount    int      `json:"node_pool_count"`
	ResourceCount    int      `json:"resource_count"`
	TotalChanges     int      `json:"total_changes"`
}

// RollbackData contém informações para rollback
type RollbackData struct {
	OriginalStateCaptured   bool `json:"original_state_captured"`
	CanRollback             bool `json:"can_rollback"`
	RollbackScriptGenerated bool `json:"rollback_script_generated"`
}

// SessionTemplate representa um template para nomenclatura de sessões
type SessionTemplate struct {
	Name        string            `json:"name"`
	Pattern     string            `json:"pattern"`
	Description string            `json:"description"`
	Variables   map[string]string `json:"variables"`
}

// OperationStatus representa o status de uma operação
type OperationStatus int

const (
	OpPending OperationStatus = iota
	OpInProgress
	OpCompleted
	OpFailed
	OpCancelled
)

func (s OperationStatus) String() string {
	switch s {
	case OpPending:
		return "⏳ Pending"
	case OpInProgress:
		return "🔄 In Progress"
	case OpCompleted:
		return "✅ Completed"
	case OpFailed:
		return "❌ Failed"
	case OpCancelled:
		return "🚫 Cancelled"
	default:
		return "❓ Unknown"
	}
}

// Operation representa uma operação sendo executada
type Operation struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`   // "update_hpa", "rollout", etc.
	Target      string          `json:"target"` // cluster/namespace/hpa
	Status      OperationStatus `json:"status"`
	Progress    float64         `json:"progress"` // 0.0 - 1.0
	Message     string          `json:"message"`
	Error       string          `json:"error,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// PanelType representa o tipo de painel ativo
type PanelType int

const (
	PanelNamespaces PanelType = iota
	PanelSelectedNamespaces
	PanelHPAs
	PanelSelectedHPAs
	PanelNodePools
	PanelSelectedNodePools
	PanelHPAMain           // Painel principal do HPA (min/max replicas, targets, rollout)
	PanelHPAResources      // Painel de recursos do deployment
)

// AppModel representa o modelo principal da aplicação
type AppModel struct {
	State          AppState
	Clusters       []Cluster
	Namespaces     []Namespace
	HPAs           []HPA
	Sessions       []Session
	CurrentSession *Session

	// UI State
	Loading       bool
	Error         string
	SuccessMsg    string
	SelectedIndex int
	PreviousState AppState // Para voltar do help

	// Snapshot completo do estado antes de entrar no Help (restaurado ao sair)
	HelpStateSnapshot *HelpStateSnapshot

	// Connectivity Status (validado periodicamente)
	VPNConnected         bool
	VPNLastCheck         time.Time
	VPNStatusMessage     string
	AzureADAuthenticated bool
	AzureADLastCheck     time.Time
	AzureADStatusMessage string

	// Modal de restart após auto-descoberta
	ShowRestartModal     bool
	RestartModalMessage  string
	RestartClustersFound int

	// Modal de erro de VPN
	ShowVPNErrorModal bool
	VPNErrorMessage   string

	// Modal de confirmação de aplicação
	ShowConfirmModal      bool
	ConfirmModalMessage   string
	ConfirmModalCallback  string // "apply_individual" ou "apply_batch"
	ConfirmModalItemCount int    // Quantidade de itens a serem aplicados

	// Panel Navigation
	ActivePanel         PanelType
	SelectedNamespaces  []Namespace
	SelectedHPAs        []HPA
	CurrentNamespaceIdx int // Index do namespace atual nos selecionados

	// Editing
	EditingHPA     *HPA
	FormFields     map[string]string
	ActiveField    string
	PerformRollout bool
	EditingField   bool   // Se está editando um campo específico
	EditingValue   string // Valor sendo editado
	CursorPosition int    // Posição do cursor no texto sendo editado

	// Session Management
	SessionName         string
	EnteringSessionName bool
	LoadedSessions      []Session
	SelectedSessionIdx  int
	LoadedSessionName   string // Nome da sessão atualmente carregada
	ConfirmingDeletion  bool   // Se está confirmando deleção de sessão
	DeletingSessionName string // Nome da sessão sendo deletada
	RenamingSession     bool   // Se está renomeando uma sessão
	RenamingSessionName string // Nome atual da sessão sendo renomeada
	NewSessionName      string // Novo nome sendo digitado

	// Session Folder Management
	SessionFolders        []string        // Lista de pastas disponíveis
	SelectedFolderIdx     int             // Índice da pasta selecionada
	LastSelectedFolderIdx int             // Última pasta selecionada (memorização ao voltar)
	CurrentFolder         string          // Pasta atualmente navegada
	SavingToFolder        bool            // Se está no processo de salvar em pasta
	FolderSessionMemory   map[string]int  // Memoriza última sessão selecionada por pasta

	// Global Position Memory System
	PositionMemory *GlobalPositionMemory // Sistema de memorização de posições

	// Current Context
	SelectedCluster *Cluster
	
	// Filters
	ShowSystemNamespaces bool
	
	// Node Pool Management
	NodePools         []NodePool
	SelectedNodePools []NodePool
	EditingNodePool   *NodePool
	
	// Azure Authentication
	IsAzureAuthenticated bool
	
	// Cluster Resource Management
	ClusterResources        []ClusterResource
	SelectedResources       []ClusterResource
	EditingResource         *ClusterResource
	ResourceFilter          ResourceType
	PrometheusStackMode     bool               // F8 mode
	ResourcePresetConfig    string             // "small", "medium", "large"
	ShowSystemResources     bool               // Toggle para mostrar recursos de sistema
	FetchingMetrics         bool               // Se está buscando métricas em background
	
	// Help Screen Navigation
	HelpScrollOffset int

	// Panel Navigation
	HPASelectedScrollOffset         int
	NodePoolSelectedScrollOffset    int
	CronJobScrollOffset             int
	ClusterScrollOffset             int
	NamespaceScrollOffset           int
	HPAListScrollOffset             int
	PrometheusStackScrollOffset     int

	// Status Container - Container reutilizável usando interface para evitar importação circular
	StatusContainer StatusContainerInterface // Container para painel de status

	// Global State Memory - Memoriza estado completo de cada painel para navegação ESC
	StateMemory map[AppState]*PanelState

	// CronJob Management
	CronJobs         []CronJob
	SelectedCronJobs []CronJob
	EditingCronJob   *CronJob

	// Add Cluster Form (F7)
	AddingCluster         bool   // Se está no modo de adicionar cluster
	AddClusterFormFields  map[string]string // Campos do formulário: "name", "resource_group", "subscription"
	AddClusterActiveField string // Campo atualmente ativo no formulário
	AddClusterFieldOrder  []string // Ordem dos campos para navegação

	// Log Viewer (F3)
	LogViewerLogs       []string // Logs carregados do arquivo
	LogViewerScrollPos  int      // Posição de scroll no log viewer
	LogViewerLoading    bool     // Se está carregando logs
	LogViewerMessage    string   // Mensagem de status do log viewer
}

// PanelState armazena o estado completo de um painel para memorização
type PanelState struct {
	State            AppState  `json:"state"`           // Estado da aplicação
	SelectedIndex    int       `json:"selected_index"`  // Posição do cursor
	ActivePanel      PanelType `json:"active_panel"`    // Tab ativo (para painéis multi-tab)
	ScrollOffset     int       `json:"scroll_offset"`   // Posição do scroll
	SubState         string    `json:"sub_state"`       // Estado adicional específico do painel
	Timestamp        time.Time `json:"timestamp"`       // Quando foi salvo
	ActionType       string    `json:"action_type"`     // "tab", "space", "enter", "esc"

	// Memorização de itens selecionados com ENTER
	SelectedCluster    string   `json:"selected_cluster"`     // Nome do cluster selecionado
	SelectedNamespaces []string `json:"selected_namespaces"`  // Lista de namespaces selecionados
	SelectedHPAs       []string `json:"selected_hpas"`        // Lista de HPAs selecionados (formato: "namespace/name")
	SelectedNodePools  []string `json:"selected_node_pools"`  // Lista de node pools selecionados
	SelectedCronJobs   []string `json:"selected_cronjobs"`    // Lista de cronjobs selecionados
	EditingItem        string   `json:"editing_item"`         // Item sendo editado (nome do HPA/NodePool/CronJob)
}

// RolloutProgress representa o progresso de um rollout
type RolloutProgress struct {
	ID          string             `json:"id"`
	HPAName     string             `json:"hpa_name"`
	Namespace   string             `json:"namespace"`
	Cluster     string             `json:"cluster"`
	RolloutType string             `json:"rollout_type"` // "deployment", "daemonset", "statefulset"
	Status      RolloutStatus      `json:"status"`
	Progress    int                `json:"progress"`     // 0-100
	Message     string             `json:"message"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     *time.Time         `json:"end_time,omitempty"`
	Error       string             `json:"error,omitempty"`
}

// RolloutStatus indica o status do rollout
type RolloutStatus int

const (
	RolloutStatusPending RolloutStatus = iota
	RolloutStatusRunning
	RolloutStatusCompleted
	RolloutStatusFailed
	RolloutStatusCancelled
)

func (s RolloutStatus) String() string {
	switch s {
	case RolloutStatusPending:
		return "pending"
	case RolloutStatusRunning:
		return "running"
	case RolloutStatusCompleted:
		return "completed"
	case RolloutStatusFailed:
		return "failed"
	case RolloutStatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// NodePoolProgress representa o progresso de uma operação de node pool
type NodePoolProgress struct {
	ID          string             `json:"id"`
	PoolName    string             `json:"pool_name"`
	ClusterName string             `json:"cluster_name"`
	Operation   string             `json:"operation"` // "scale", "autoscale", "update"
	Status      RolloutStatus      `json:"status"`
	Progress    int                `json:"progress"`     // 0-100
	Message     string             `json:"message"`
	StartTime   time.Time          `json:"start_time"`
	EndTime     *time.Time         `json:"end_time,omitempty"`
	Error       string             `json:"error,omitempty"`

	// Detalhes da operação
	FromNodeCount int32 `json:"from_node_count"`
	ToNodeCount   int32 `json:"to_node_count"`
	FromMinNodes  int32 `json:"from_min_nodes"`
	ToMinNodes    int32 `json:"to_min_nodes"`
	FromMaxNodes  int32 `json:"from_max_nodes"`
	ToMaxNodes    int32 `json:"to_max_nodes"`
}

// NodePool representa um node pool do Azure AKS
type NodePool struct {
	Name         string `json:"name"`
	VMSize       string `json:"vm_size"`
	NodeCount    int32  `json:"node_count"`
	MinNodeCount int32  `json:"min_node_count"`
	MaxNodeCount int32  `json:"max_node_count"`
	AutoscalingEnabled bool `json:"autoscaling_enabled"`
	Status       string `json:"status"`
	IsSystemPool bool   `json:"is_system_pool"`
	Modified     bool   `json:"modified"`
	Selected     bool   `json:"selected"`
	AppliedCount int    `json:"applied_count"` // Contador de quantas vezes foi aplicado

	// Sistema de execução sequencial para stress tests (máximo 2 nodes)
	SequenceOrder  int    `json:"sequence_order"`  // 1 = primeiro, 2 = segundo, 0 = não marcado
	SequenceStatus string `json:"sequence_status"` // pending, executing, completed, failed

	// Cluster info
	ClusterName   string `json:"cluster_name"`
	ResourceGroup string `json:"resource_group"`
	Subscription  string `json:"subscription"`

	// Valores originais para rollback
	OriginalValues NodePoolValues `json:"original_values"`
}

// NodePoolValues valores originais do node pool
type NodePoolValues struct {
	NodeCount    int32  `json:"node_count"`
	MinNodeCount int32  `json:"min_node_count"`
	MaxNodeCount int32  `json:"max_node_count"`
	AutoscalingEnabled bool `json:"autoscaling_enabled"`
}

// NodePoolChange representa uma mudança em node pool
type NodePoolChange struct {
	Cluster       string          `json:"cluster"`
	ResourceGroup string          `json:"resource_group"`
	Subscription  string          `json:"subscription"`
	NodePoolName  string          `json:"node_pool_name"`
	OriginalValues NodePoolValues `json:"original_values"`
	NewValues      NodePoolValues `json:"new_values"`
	Applied       bool            `json:"applied"`
	AppliedAt     *time.Time      `json:"applied_at,omitempty"`
	Error         string          `json:"error,omitempty"`

	// Campos de execução sequencial
	SequenceOrder  int    `json:"sequence_order"`  // 1 = primeiro, 2 = segundo, 0 = não marcado
	SequenceStatus string `json:"sequence_status"` // pending, executing, completed, failed
}

// ResourceType representa o tipo de recurso do cluster
type ResourceType int

const (
	ResourceMonitoring ResourceType = iota // Prometheus, Grafana
	ResourceIngress                        // Nginx, Istio
	ResourceSecurity                       // Cert-manager, Gatekeeper
	ResourceStorage                        // Longhorn, etc.
	ResourceNetworking                     // Calico, Metallb
	ResourceLogging                        // Elastic, Fluentd
	ResourceCustom                         // Apps do usuário em namespaces system
)

func (r ResourceType) String() string {
	switch r {
	case ResourceMonitoring:
		return "📊 Monitoring"
	case ResourceIngress:
		return "🌐 Ingress"
	case ResourceSecurity:
		return "🔒 Security"
	case ResourceStorage:
		return "📦 Storage"
	case ResourceNetworking:
		return "🌐 Network"
	case ResourceLogging:
		return "📝 Logging"
	case ResourceCustom:
		return "⚙️ Custom"
	default:
		return "❓ Unknown"
	}
}

// ResourceStatus representa o status do recurso
type ResourceStatus int

const (
	ResourceHealthy ResourceStatus = iota
	ResourceWarning
	ResourceCritical
	ResourceUpdating
)

func (s ResourceStatus) String() string {
	switch s {
	case ResourceHealthy:
		return "✅ Healthy"
	case ResourceWarning:
		return "⚠️ Warning"
	case ResourceCritical:
		return "❌ Critical"
	case ResourceUpdating:
		return "🔄 Updating"
	default:
		return "❓ Unknown"
	}
}

// ClusterResource representa um recurso de sistema do cluster
type ClusterResource struct {
	Name         string        `json:"name"`
	Namespace    string        `json:"namespace"`
	Type         ResourceType  `json:"type"`
	Component    string        `json:"component"`    // prometheus-server, grafana, etc.
	WorkloadType string        `json:"workload_type"` // Deployment, DaemonSet, StatefulSet
	
	// Recursos atuais (requests)
	CurrentCPURequest    string `json:"current_cpu_request"`
	CurrentMemoryRequest string `json:"current_memory_request"`

	// Limites atuais
	CurrentCPULimit    string `json:"current_cpu_limit,omitempty"`
	CurrentMemoryLimit string `json:"current_memory_limit,omitempty"`

	// Campos para exibição com métricas de uso (não são editáveis)
	DisplayCPURequest    string `json:"-"` // Ex: "1 (uso: 264m)" - apenas para visualização
	DisplayMemoryRequest string `json:"-"` // Ex: "8Gi (uso: 3918Mi)" - apenas para visualização
	
	// Valores desejados (requests)
	TargetCPURequest    string `json:"target_cpu_request,omitempty"`
	TargetMemoryRequest string `json:"target_memory_request,omitempty"`
	
	// Valores desejados (limits)
	TargetCPULimit    string `json:"target_cpu_limit,omitempty"`
	TargetMemoryLimit string `json:"target_memory_limit,omitempty"`
	
	// Configuração
	Replicas       int32  `json:"replicas"`
	StorageSize    string `json:"storage_size,omitempty"`
	TargetReplicas *int32 `json:"target_replicas,omitempty"`
	
	// Estado
	Modified       bool           `json:"modified"`
	Selected       bool           `json:"selected"`
	Status         ResourceStatus `json:"status"`
	Cluster        string         `json:"cluster"`
	
	// Valores originais para rollback
	OriginalValues *ResourceValues `json:"original_values"`
	LastUpdated    time.Time       `json:"last_updated"`
}

// ResourceValues armazena valores originais para rollback
type ResourceValues struct {
	CPURequest    string `json:"cpu_request"`
	MemoryRequest string `json:"memory_request"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
	Replicas      int32  `json:"replicas"`
	StorageSize   string `json:"storage_size,omitempty"`
}

// ClusterResourceChange representa mudança em recurso do cluster
type ClusterResourceChange struct {
	Cluster        string          `json:"cluster"`
	Namespace      string          `json:"namespace"`
	ResourceName   string          `json:"resource_name"`
	WorkloadType   string          `json:"workload_type"`
	Component      string          `json:"component"`
	OriginalValues *ResourceValues `json:"original_values"`
	NewValues      *ResourceValues `json:"new_values"`
	Applied        bool            `json:"applied"`
	AppliedAt      *time.Time      `json:"applied_at,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// ClusterConfig representa a configuração de um cluster no arquivo clusters-config.json
type ClusterConfig struct {
	ClusterName   string `json:"clusterName"`
	ResourceGroup string `json:"resourceGroup"`
	Subscription  string `json:"subscription"`
}

// CronJob representa um CronJob do Kubernetes
type CronJob struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Cluster           string    `json:"cluster"`
	Schedule          string    `json:"schedule"`
	ScheduleDesc      string    `json:"schedule_description"` // Descrição legível do schedule
	Suspend           *bool     `json:"suspend"`              // Se está suspenso (desabilitado)
	LastScheduleTime  *time.Time `json:"last_schedule_time"`
	LastRunStatus     string    `json:"last_run_status"`      // "Success", "Failed", "Unknown"
	ActiveJobs        int       `json:"active_jobs"`
	SuccessfulJobs    int32     `json:"successful_jobs"`
	FailedJobs        int32     `json:"failed_jobs"`
	JobTemplate       string    `json:"job_template"`         // Nome/descrição do template
	Selected          bool      `json:"selected"`
	Modified          bool      `json:"modified"`
	OriginalSuspend   *bool     `json:"original_suspend"`     // Valor original para rollback
}

// CronJobStatus representa o status de execução de um CronJob
type CronJobStatus string

const (
	CronJobStatusSuccess CronJobStatus = "Success"
	CronJobStatusFailed  CronJobStatus = "Failed"
	CronJobStatusUnknown CronJobStatus = "Unknown"
	CronJobStatusRunning CronJobStatus = "Running"
)

// ===== FUNÇÕES HELPER PARA SISTEMA DE MEMORIZAÇÃO DE POSIÇÕES =====

// InitializePositionMemory inicializa o sistema de memorização se não existir
func (m *AppModel) InitializePositionMemory() {
	if m.PositionMemory == nil {
		m.PositionMemory = &GlobalPositionMemory{}
	}
}

// MemorizeCurrentPosition memoriza a posição atual com o tipo de ação
func (m *AppModel) MemorizeCurrentPosition(actionType string) {
	m.InitializePositionMemory()

	currentState := &PanelState{
		State:         m.State,
		SelectedIndex: m.SelectedIndex,
		ActivePanel:   m.ActivePanel,
		Timestamp:     time.Now(),
		ActionType:    actionType,
	}

	// Salvar como estado anterior (para ESC)
	m.PositionMemory.PreviousState = currentState

	// Salvar na memória específica da ação
	switch actionType {
	case "tab":
		m.PositionMemory.TabMemory = currentState
	case "space":
		m.PositionMemory.SpaceMemory = currentState
	case "enter":
		m.PositionMemory.EnterMemory = currentState
	}

	// Atualizar última ação
	m.PositionMemory.LastAction = actionType
	m.PositionMemory.LastActionTime = time.Now()
}

// RestorePreviousPosition restaura a posição anterior (para ESC)
func (m *AppModel) RestorePreviousPosition() bool {
	m.InitializePositionMemory()

	if m.PositionMemory.PreviousState == nil {
		return false
	}

	// Restaurar posição
	m.State = m.PositionMemory.PreviousState.State
	m.SelectedIndex = m.PositionMemory.PreviousState.SelectedIndex
	m.ActivePanel = m.PositionMemory.PreviousState.ActivePanel

	return true
}

// RestoreTabPosition restaura a posição memorizada do TAB
func (m *AppModel) RestoreTabPosition() bool {
	m.InitializePositionMemory()

	if m.PositionMemory.TabMemory == nil {
		return false
	}

	// Restaurar posição do TAB
	m.State = m.PositionMemory.TabMemory.State
	m.SelectedIndex = m.PositionMemory.TabMemory.SelectedIndex
	m.ActivePanel = m.PositionMemory.TabMemory.ActivePanel

	return true
}

// RestoreSpacePosition restaura a posição memorizada do SPACE
func (m *AppModel) RestoreSpacePosition() bool {
	m.InitializePositionMemory()

	if m.PositionMemory.SpaceMemory == nil {
		return false
	}

	// Restaurar posição do SPACE
	m.State = m.PositionMemory.SpaceMemory.State
	m.SelectedIndex = m.PositionMemory.SpaceMemory.SelectedIndex
	m.ActivePanel = m.PositionMemory.SpaceMemory.ActivePanel

	return true
}

// RestoreEnterPosition restaura a posição memorizada do ENTER
func (m *AppModel) RestoreEnterPosition() bool {
	m.InitializePositionMemory()

	if m.PositionMemory.EnterMemory == nil {
		return false
	}

	// Restaurar posição do ENTER
	m.State = m.PositionMemory.EnterMemory.State
	m.SelectedIndex = m.PositionMemory.EnterMemory.SelectedIndex
	m.ActivePanel = m.PositionMemory.EnterMemory.ActivePanel

	return true
}

// GetLastAction retorna a última ação memorizada
func (m *AppModel) GetLastAction() string {
	m.InitializePositionMemory()
	return m.PositionMemory.LastAction
}

// ===== STATUS PANEL MODULE =====

// StatusPanelModule - Módulo dedicado para painel de status
type StatusPanelModule struct {
	// Configuração fixa
	width  int // 140 fixo
	height int // 15 fixo
	title  string

	// Mensagens e controle
	messages    []StatusMessage
	scrollPos   int  // Posição do scroll (primeira linha visível)
	focused     bool // Se está focado para scroll
	lastUpdate  time.Time

	// Controle de exibição
	maxVisibleLines int // 11 linhas visíveis (15 - 4 para bordas e título)
}

// StatusMessage - Estrutura de mensagem do painel
type StatusMessage struct {
	Timestamp time.Time
	Level     MessageLevel
	Source    string
	Content   string
}

// MessageLevel - Níveis de mensagem
type MessageLevel int

const (
	LevelInfo MessageLevel = iota
	LevelSuccess
	LevelWarning
	LevelError
	LevelDebug
)

// NewStatusPanelModule - Criar novo módulo de painel de status
func NewStatusPanelModule(title string) *StatusPanelModule {
	return &StatusPanelModule{
		width:           140, // Largura solicitada pelo usuário
		height:          15,  // 15 linhas TOTAL (13 conteúdo + 2 bordas)
		title:           title,
		messages:        make([]StatusMessage, 0),
		scrollPos:       0,
		focused:         false,
		lastUpdate:      time.Now(),
		maxVisibleLines: 13, // 13 linhas de conteúdo (15 - 2 bordas)
	}
}

// AddMessage - Adicionar nova mensagem ao painel
func (sp *StatusPanelModule) AddMessage(level MessageLevel, source, content string) {
	msg := StatusMessage{
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Content:   content,
	}

	sp.messages = append(sp.messages, msg)
	sp.lastUpdate = time.Now()

	// Auto-scroll para a última mensagem (foco automático na nova mensagem)
	sp.scrollToBottom()
}

// Métodos convenientes para adicionar mensagens
func (sp *StatusPanelModule) Info(source, content string) {
	sp.AddMessage(LevelInfo, source, content)
}

func (sp *StatusPanelModule) Success(source, content string) {
	sp.AddMessage(LevelSuccess, source, content)
}

func (sp *StatusPanelModule) Warning(source, content string) {
	sp.AddMessage(LevelWarning, source, content)
}

func (sp *StatusPanelModule) Error(source, content string) {
	sp.AddMessage(LevelError, source, content)
}

func (sp *StatusPanelModule) Debug(source, content string) {
	sp.AddMessage(LevelDebug, source, content)
}

// SetFocused - Ativar/desativar foco para scroll
func (sp *StatusPanelModule) SetFocused(focused bool) {
	sp.focused = focused
}

// IsFocused - Verificar se está focado
func (sp *StatusPanelModule) IsFocused() bool {
	return sp.focused
}

// ScrollUp - Scroll para cima (apenas se focado)
func (sp *StatusPanelModule) ScrollUp() {
	if !sp.focused {
		return
	}

	if sp.scrollPos > 0 {
		sp.scrollPos--
	}
}

// ScrollDown - Scroll para baixo (apenas se focado)
func (sp *StatusPanelModule) ScrollDown() {
	if !sp.focused {
		return
	}

	totalMessages := len(sp.messages)
	if totalMessages <= sp.maxVisibleLines {
		return // Não precisa de scroll
	}

	maxScroll := totalMessages - sp.maxVisibleLines
	if sp.scrollPos < maxScroll {
		sp.scrollPos++
	}
}

// scrollToBottom - Auto-scroll para a última mensagem
func (sp *StatusPanelModule) scrollToBottom() {
	totalMessages := len(sp.messages)
	if totalMessages <= sp.maxVisibleLines {
		sp.scrollPos = 0
		return
	}

	// Posicionar para mostrar as últimas mensagens
	sp.scrollPos = totalMessages - sp.maxVisibleLines
}

// HandleMouseClick - Processar clique do mouse para ativar foco
func (sp *StatusPanelModule) HandleMouseClick(x, y int, panelX, panelY int) bool {
	// Verificar se o clique está dentro da área do painel
	if x >= panelX && x < panelX+sp.width &&
	   y >= panelY && y < panelY+sp.height {
		sp.SetFocused(true)
		return true // Clique capturado
	}

	// Clique fora do painel - remover foco
	sp.SetFocused(false)
	return false
}

// Render - Renderizar o painel completo
func (sp *StatusPanelModule) Render() string {
	// Obter mensagens visíveis baseado no scroll
	visibleMessages := sp.getVisibleMessages()

	// Renderizar conteúdo das mensagens
	content := sp.renderMessages(visibleMessages)

	// Adicionar indicador de scroll se necessário
	scrollIndicator := sp.getScrollIndicator()

	// Título com indicador de foco e scroll
	title := sp.title
	if sp.focused {
		title += " [FOCADO]"
	}
	if scrollIndicator != "" {
		title += " " + scrollIndicator
	}

	// Renderizar painel com bordas
	return sp.renderPanelWithBorder(content, title)
}

// getVisibleMessages - Obter mensagens visíveis baseado no scroll
func (sp *StatusPanelModule) getVisibleMessages() []StatusMessage {
	totalMessages := len(sp.messages)

	if totalMessages == 0 {
		return []StatusMessage{}
	}

	// Se todas as mensagens cabem, mostrar todas
	if totalMessages <= sp.maxVisibleLines {
		return sp.messages
	}

	// Aplicar scroll
	start := sp.scrollPos
	end := start + sp.maxVisibleLines

	if end > totalMessages {
		end = totalMessages
	}

	return sp.messages[start:end]
}

// renderMessages - Renderizar lista de mensagens
func (sp *StatusPanelModule) renderMessages(messages []StatusMessage) string {
	if len(messages) == 0 {
		return "Sistema pronto - aguardando atividade..."
	}

	var lines []string

	for _, msg := range messages {
		line := sp.formatMessage(msg)
		lines = append(lines, line)
	}

	// Não preencher linhas vazias - deixar o painel se ajustar ao conteúdo
	// Máximo de 13 linhas será exibido, mas o painel pode ser menor se houver menos mensagens

	return strings.Join(lines, "\n")
}

// formatMessage - Formatar uma mensagem individual
func (sp *StatusPanelModule) formatMessage(msg StatusMessage) string {
	// Ícone baseado no nível
	icon := sp.getMessageIcon(msg.Level)

	// Timestamp simples
	timeStr := msg.Timestamp.Format("15:04:05")

	// Truncar conteúdo se necessário para caber na largura
	maxContentWidth := sp.width - 20 // Espaço para timestamp, ícone, source, etc.
	content := msg.Content
	if len(content) > maxContentWidth {
		content = content[:maxContentWidth-3] + "..."
	}

	// Formato: [15:04:05] 🔵 source: conteúdo
	return fmt.Sprintf("[%s] %s %s: %s", timeStr, icon, msg.Source, content)
}

// getMessageIcon - Obter ícone baseado no nível da mensagem
func (sp *StatusPanelModule) getMessageIcon(level MessageLevel) string {
	switch level {
	case LevelSuccess:
		return "✅"
	case LevelWarning:
		return "⚠️"
	case LevelError:
		return "❌"
	case LevelDebug:
		return "🔍"
	default: // LevelInfo
		return "ℹ️"
	}
}

// getScrollIndicator - Obter indicador de scroll
func (sp *StatusPanelModule) getScrollIndicator() string {
	totalMessages := len(sp.messages)

	if totalMessages <= sp.maxVisibleLines {
		return "" // Não precisa de scroll
	}

	start := sp.scrollPos + 1
	end := sp.scrollPos + sp.maxVisibleLines

	return fmt.Sprintf("[%d-%d/%d]", start, end, totalMessages)
}

// renderPanelWithBorder - Renderizar painel com bordas próprias (sistema unificado)
func (sp *StatusPanelModule) renderPanelWithBorder(content, title string) string {
	// Dividir conteúdo em linhas
	lines := strings.Split(content, "\n")

	// Dimensões do painel: 140 largura x 15 altura
	width := 140
	height := 15

	// Calcular padding para centralizar título
	titleLength := len([]rune(title))
	leftDashes := (width - titleLength - 6) / 2 // 6 = espaços e caracteres de borda
	if leftDashes < 0 {
		leftDashes = 0
	}
	rightDashes := width - titleLength - leftDashes - 6

	// Bordas
	topBorder := "╭" + strings.Repeat("─", leftDashes) + " " + title + " " + strings.Repeat("─", rightDashes) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", width-2) + "╯"

	// Processar linhas de conteúdo
	contentWidth := width - 4 // espaço para bordas laterais
	var contentLines []string

	for _, line := range lines {
		// Truncar se muito longo
		if len(line) > contentWidth {
			line = line[:contentWidth-3] + "..."
		}
		// Padding para completar largura
		padding := contentWidth - len([]rune(line))
		paddedLine := "│ " + line + strings.Repeat(" ", padding) + " │"
		contentLines = append(contentLines, paddedLine)
	}

	// Preencher linhas vazias até altura desejada
	emptyLine := "│" + strings.Repeat(" ", width-2) + "│"
	desiredContentLines := height - 2 // -2 para bordas superior/inferior
	for len(contentLines) < desiredContentLines {
		contentLines = append(contentLines, emptyLine)
	}

	// Montar resultado final
	var result []string
	result = append(result, topBorder)
	result = append(result, contentLines...)
	result = append(result, bottomBorder)

	return strings.Join(result, "\n")
}

// Clear - Limpar todas as mensagens
func (sp *StatusPanelModule) Clear() {
	sp.messages = make([]StatusMessage, 0)
	sp.scrollPos = 0
	sp.lastUpdate = time.Now()
}

// GetMessageCount - Obter número total de mensagens
func (sp *StatusPanelModule) GetMessageCount() int {
	return len(sp.messages)
}

// GetLastUpdate - Obter timestamp da última atualização
func (sp *StatusPanelModule) GetLastUpdate() time.Time {
	return sp.lastUpdate
}

// ===== MÉTODOS DE COMPATIBILIDADE COM INTERFACE ANTIGA =====

// AddProgressBar - Compatibilidade com interface antiga (simplificado)
func (sp *StatusPanelModule) AddProgressBar(id, title string, total int) interface{} {
	sp.Info("progress", fmt.Sprintf("🔄 %s - iniciado", title))
	return nil // Retorna um placeholder
}

// UpdateProgress - Compatibilidade com interface antiga (simplificado)
func (sp *StatusPanelModule) UpdateProgress(id string, current int, status string) {
	percentage := current // Assumir que current já é percentual
	if status == "completed" {
		sp.Success("progress", fmt.Sprintf("✅ Progresso %s: 100%% concluído", id))
	} else if status == "failed" {
		sp.Error("progress", fmt.Sprintf("❌ Progresso %s: falhou em %d%%", id, percentage))
	} else if current%20 == 0 { // Mostrar apenas a cada 20%
		sp.Info("progress", fmt.Sprintf("🔄 Progresso %s: %d%%", id, percentage))
	}
}

// HPARollout - Log específico para rollout de HPA
func (sp *StatusPanelModule) HPARollout(cluster, namespace, name, rolloutType string) {
	sp.Info("rollout", fmt.Sprintf("🔄 Iniciando rollout %s: %s/%s/%s", rolloutType, cluster, namespace, name))
}

// NodePoolScaling - Log específico para scaling de node pool
func (sp *StatusPanelModule) NodePoolScaling(cluster, pool string, from, to int) {
	sp.Info("nodepool", fmt.Sprintf("📊 Scaling %s/%s: %d → %d nodes", cluster, pool, from, to))
}
