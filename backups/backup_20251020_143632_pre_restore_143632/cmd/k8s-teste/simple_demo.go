package main

import (
	"time"

	"k8s-hpa-manager/internal/models"
	"k8s-hpa-manager/internal/tui/components"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SimpleDemo é um modelo simples para demonstrar o layout unificado
type SimpleDemo struct {
	container     *components.UnifiedContainer
	currentState  models.AppState
	stateIndex    int
	width, height int
}

// NewSimpleDemo cria uma nova demo simples
func NewSimpleDemo() *SimpleDemo {
	return &SimpleDemo{
		container:    components.NewUnifiedContainer(),
		currentState: models.StateClusterSelection,
		stateIndex:   0,
	}
}

// Init implementa tea.Model
func (sd *SimpleDemo) Init() tea.Cmd {
	return tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update implementa tea.Model
func (sd *SimpleDemo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		sd.width = msg.Width
		sd.height = msg.Height
		return sd, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "f4":
			return sd, tea.Quit

		case "tab", "right", "l":
			// Avança para próximo estado
			sd.nextState()
			return sd, nil

		case "shift+tab", "left", "h":
			// Volta para estado anterior
			sd.prevState()
			return sd, nil

		case "r":
			// Reset para primeiro estado
			sd.stateIndex = 0
			sd.currentState = models.StateClusterSelection
			return sd, nil
		}

	case tickMsg:
		// Avança automaticamente para próximo estado a cada 3 segundos
		sd.nextState()

		// Adiciona uma mensagem de status baseada no estado atual
		sd.addStatusForCurrentState()

		return sd, tea.Tick(time.Second*3, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}

	return sd, nil
}

// View implementa tea.Model
func (sd *SimpleDemo) View() string {
	// Configurar o container com o estado atual
	sd.container.SetTitle(sd.currentState)

	// Gerar conteúdo de demonstração baseado no estado
	content := sd.generateDemoContent()
	sd.container.SetContent(content)

	// Renderizar o container
	containerView := sd.container.Render()

	// Adicionar instruções de controle
	controls := sd.renderControls()

	return lipgloss.JoinVertical(lipgloss.Left, containerView, controls)
}

// nextState avança para o próximo estado
func (sd *SimpleDemo) nextState() {
	states := []models.AppState{
		models.StateClusterSelection,
		models.StateSessionSelection,
		models.StateNamespaceSelection,
		models.StateHPASelection,
		models.StateHPAEditing,
		models.StateNodeSelection,
		models.StateNodeEditing,
		models.StateCronJobSelection,
	}

	sd.stateIndex = (sd.stateIndex + 1) % len(states)
	sd.currentState = states[sd.stateIndex]
}

// prevState volta para o estado anterior
func (sd *SimpleDemo) prevState() {
	states := []models.AppState{
		models.StateClusterSelection,
		models.StateSessionSelection,
		models.StateNamespaceSelection,
		models.StateHPASelection,
		models.StateHPAEditing,
		models.StateNodeSelection,
		models.StateNodeEditing,
		models.StateCronJobSelection,
	}

	sd.stateIndex--
	if sd.stateIndex < 0 {
		sd.stateIndex = len(states) - 1
	}
	sd.currentState = states[sd.stateIndex]
}

// generateDemoContent gera conteúdo de demonstração para cada estado
func (sd *SimpleDemo) generateDemoContent() string {
	switch sd.currentState {
	case models.StateClusterSelection:
		return `🌐 CLUSTERS KUBERNETES DISPONÍVEIS

► ✅ akspriv-dev-central     (Conectado)
  ⏳ akspriv-prod-east       (Verificando...)
  ❌ akspriv-test-west       (Erro de conexão)
  ✅ akspriv-staging-north   (Conectado)

📊 INFORMAÇÕES DO SISTEMA
├─ Clusters descobertos: 4
├─ Kubeconfig: ~/.kube/config
├─ Contexto atual: akspriv-dev-central
└─ Status: Pronto para seleção

🎮 CONTROLES DISPONÍVEIS
• ↑↓ Navegar entre clusters
• ENTER Selecionar cluster
• Ctrl+L Carregar sessão salva
• F5 Recarregar lista de clusters`

	case models.StateSessionSelection:
		return `💾 GERENCIAMENTO DE SESSÕES

📂 SESSÕES DISPONÍVEIS

► 📄 hpa-upscale-prod-29-12-24_14:30:15
   📝 Escalonamento HPAs ambiente produção
   📅 29/12/2024 14:30 | 🎯 12 HPAs | 🔧 5 Node Pools

  📄 emergency-downscale-28-12-24_22:45:10
   📝 Redução emergencial recursos cluster
   📅 28/12/2024 22:45 | 🎯 8 HPAs | 🔧 3 Node Pools

  📄 maintenance-window-27-12-24_02:00:00
   📝 Janela de manutenção programada
   📅 27/12/2024 02:00 | 🎯 20 HPAs | 🔧 8 Node Pools

🎮 OPERAÇÕES DISPONÍVEIS
• ↑↓ Navegar entre sessões
• ENTER Carregar sessão
• Ctrl+N/F2 Renomear sessão
• Ctrl+R Deletar sessão`

	case models.StateNamespaceSelection:
		return `📋 SELEÇÃO DE NAMESPACES
Cluster: akspriv-dev-central

📦 NAMESPACES DISPONÍVEIS

► [✓] api-services              (15 HPAs)
  [✓] web-frontend             (8 HPAs)
  [ ] database                 (3 HPAs)
  [ ] monitoring               (2 HPAs)
  [✓] ingress-nginx           (1 HPA)
  [ ] default                  (0 HPAs)

✅ NAMESPACES SELECIONADOS (3)
• api-services    → 15 HPAs detectados
• web-frontend    → 8 HPAs detectados
• ingress-nginx   → 1 HPA detectado

📊 RESUMO DA SELEÇÃO
├─ Total de HPAs: 24
├─ Namespaces ativos: 3/6
└─ Pronto para continuar

🎮 CONTROLES
• ↑↓ Navegar • SPACE Selecionar • ENTER Confirmar`

	case models.StateHPASelection:
		return `🎯 GERENCIAMENTO DE HPAs
Cluster: akspriv-dev-central | Namespaces: 3 selecionados

───── api-services (15 HPAs) ─────
► [✓] api-gateway           Min:2 Max:10 Curr:5  ●3
  [✓] user-service         Min:1 Max:8  Curr:3  ✨
  [ ] payment-api          Min:2 Max:12 Curr:4
  [✓] notification-svc     Min:1 Max:6  Curr:2  ●1

───── web-frontend (8 HPAs) ─────
  [✓] react-app            Min:3 Max:15 Curr:8  ✨●2
  [ ] nginx-proxy          Min:2 Max:8  Curr:4
  [✓] static-assets        Min:1 Max:5  Curr:2

───── ingress-nginx (1 HPA) ─────
  [✓] nginx-controller     Min:2 Max:6  Curr:3  ●1

📊 STATUS ATUAL
├─ HPAs selecionados: 6/24
├─ Modificados: 2 ✨
├─ Aplicados: 4 com múltiplas execuções ●
└─ Rollouts habilitados: Deploy, DaemonSet, StatefulSet

🎮 OPERAÇÕES
• Ctrl+D Aplicar individual • Ctrl+U Aplicar batch • ENTER Editar`

	case models.StateHPAEditing:
		return `✏️  EDITANDO HPA: api-gateway
Cluster: akspriv-dev-central | Namespace: api-services

📊 CONFIGURAÇÕES PRINCIPAIS
├─ Min Replicas:     2 → 3      ✨
├─ Max Replicas:     10 → 15    ✨
├─ Target CPU:       70% → 65%  ✨
└─ Target Memory:    80% → 75%  ✨

🔄 OPÇÕES DE ROLLOUT
├─ Deployment:       ✅ Habilitado
├─ DaemonSet:        ❌ Desabilitado
└─ StatefulSet:      ✅ Habilitado

📦 RECURSOS DO DEPLOYMENT
├─ CPU Request:      100m → 150m ✨
├─ CPU Limit:        500m → 800m ✨
├─ Memory Request:   128Mi → 256Mi ✨
└─ Memory Limit:     512Mi → 1Gi ✨

🎮 CONTROLES DE EDIÇÃO
• ↑↓ Navegar campos • ENTER Editar valor
• SPACE Toggle rollout • TAB Alternar painel
• Ctrl+S Salvar alterações • ESC Voltar`

	case models.StateNodeSelection:
		return `🔧 GERENCIAMENTO DE NODE POOLS
Cluster: akspriv-dev-central | Resource Group: dev-aks-rg

📊 NODE POOLS DISPONÍVEIS

► [✓] system-pool           Nodes:3   Min:1  Max:5    [autoscale] ●2
  [✓] worker-pool           Nodes:8   Min:3  Max:20   [autoscale] ✨
  [ ] gpu-pool              Nodes:0   Min:0  Max:10   [manual]
  [✓] spot-pool             Nodes:5   Min:2  Max:15   [autoscale] *1
  [ ] memory-pool           Nodes:2   Min:1  Max:8    [autoscale] *2

📈 EXECUÇÃO SEQUENCIAL CONFIGURADA
├─ Primeiro: spot-pool (*1)     → Executar manualmente
└─ Segundo:  memory-pool (*2)   → Auto-start após conclusão

💰 ESTIMATIVA DE CUSTOS
├─ Configuração atual: ~$450/mês
├─ Após alterações:    ~$380/mês
└─ Economia projetada: ~$70/mês (15.5%)

🎮 OPERAÇÕES AZURE
• Ctrl+D Aplicar individual • Ctrl+U Aplicar batch
• F12 Marcar sequencial • ENTER Editar configurações`

	case models.StateNodeEditing:
		return `✏️  EDITANDO NODE POOL: worker-pool
Cluster: akspriv-dev-central | Subscription: dev-subscription

⚙️  CONFIGURAÇÕES ATUAIS
├─ Autoscaling:      ✅ Habilitado
├─ Node Count:       8 (atual)
├─ Min Nodes:        3 → 5      ✨
├─ Max Nodes:        20 → 25    ✨
└─ VM Size:          Standard_D4s_v3

🔧 CONFIGURAÇÕES AZURE
├─ Availability Set: Habilitado
├─ Ultra SSD:        Desabilitado
├─ OS Disk Type:     Premium SSD
└─ Network Policy:   Azure CNI

💡 IMPACT ANALYSIS
├─ Aumento min nodes: +2 nodes
├─ Custo adicional:   ~$120/mês
├─ Maior estabilidade: ✅
└─ Failover melhor:   ✅

🎮 CONTROLES
• ↑↓ Navegar • ENTER Editar • SPACE Toggle autoscaling
• Ctrl+S Aplicar via Azure CLI • ESC Cancelar`

	case models.StateCronJobSelection:
		return `⏰ GERENCIAMENTO DE CRONJOBS
Cluster: akspriv-dev-central | Namespace: All

📅 CRONJOBS DISPONÍVEIS

► [✓] backup-database          0 2 * * *    🟢 Ativo
      Executa todo dia às 2:00 AM
      Última exec: 29/12 02:00 ✅

  [✓] cleanup-logs            0 4 * * 0     🟢 Ativo
      Executa domingo às 4:00 AM
      Última exec: 24/12 04:00 ✅

  [ ] maintenance-check       */30 * * * *  🔴 Suspenso
      Executa a cada 30 minutos
      Última exec: 20/12 14:30 ⚠️

  [✓] report-generation       0 8 1 * *     🟢 Ativo
      Executa 1º dia do mês às 8:00 AM
      Última exec: 01/12 08:00 ✅

📊 ESTATÍSTICAS
├─ Total CronJobs: 4
├─ Ativos: 3 🟢
├─ Suspensos: 1 🔴
└─ Próxima exec: backup-database em 8h

🎮 OPERAÇÕES
• ↑↓ Navegar • SPACE Selecionar • ENTER Editar
• Ctrl+D Aplicar individual • Ctrl+U Aplicar batch`

	default:
		return `🚀 K8S HPA MANAGER - CONTAINER UNIFICADO

📐 DEMONSTRAÇÃO DO LAYOUT
• Dimensões fixas: 230x70 caracteres
• Header dinâmico baseado no estado atual
• Container único com moldura elegante
• Quebra de linhas inteligente para texto longo

🎨 RECURSOS IMPLEMENTADOS
• ✅ Container unificado com Lipgloss
• ✅ Header contextual automático
• ✅ Moldura responsiva e elegante
• ✅ Integração com Bubble Tea
• ✅ Quebra de texto inteligente
• ✅ Layout limpo e profissional

Esta é uma demonstração visual do novo layout unificado.
O sistema mantém toda a lógica original intacta.`
	}
}

// renderControls renderiza os controles da demonstração
func (sd *SimpleDemo) renderControls() string {
	controlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Margin(1, 0, 0, 0)

	controls := `🎮 CONTROLES DA DEMONSTRAÇÃO
→/L: Próximo estado • ←/H: Estado anterior • R: Reset • Q/Ctrl+C/F4: Sair
Auto-avanço: 3 segundos | Estado atual: ` + sd.getStateName()

	return controlStyle.Render(controls)
}

// getStateName retorna o nome amigável do estado atual
func (sd *SimpleDemo) getStateName() string {
	switch sd.currentState {
	case models.StateClusterSelection:
		return "Seleção de Clusters"
	case models.StateSessionSelection:
		return "Gerenciamento de Sessões"
	case models.StateNamespaceSelection:
		return "Seleção de Namespaces"
	case models.StateHPASelection:
		return "Gerenciamento de HPAs"
	case models.StateHPAEditing:
		return "Editando HPA"
	case models.StateNodeSelection:
		return "Gerenciamento de Node Pools"
	case models.StateNodeEditing:
		return "Editando Node Pool"
	case models.StateCronJobSelection:
		return "Gerenciamento de CronJobs"
	default:
		return "Estado Desconhecido"
	}
}

// addStatusForCurrentState adiciona mensagens de status baseadas no estado atual
func (sd *SimpleDemo) addStatusForCurrentState() {
	switch sd.currentState {
	case models.StateClusterSelection:
		sd.container.AddStatusMessage(components.MessageInfo, "cluster", "Descobrindo clusters akspriv-*...")
	case models.StateSessionSelection:
		sd.container.AddStatusMessage(components.MessageSuccess, "session", "Sessões carregadas com sucesso")
	case models.StateNamespaceSelection:
		sd.container.AddStatusMessage(components.MessageInfo, "namespace", "Contando HPAs por namespace...")
	case models.StateHPASelection:
		sd.container.AddStatusMessage(components.MessageSuccess, "hpa", "24 HPAs encontrados em 3 namespaces")
	case models.StateHPAEditing:
		sd.container.AddStatusMessage(components.MessageWarning, "edit", "Modificações pendentes - lembre de salvar")
	case models.StateNodeSelection:
		sd.container.AddStatusMessage(components.MessageInfo, "azure", "Conectando com Azure AKS...")
	case models.StateNodeEditing:
		sd.container.AddStatusMessage(components.MessageSuccess, "azure", "Node pool configurado com sucesso")
	case models.StateCronJobSelection:
		sd.container.AddStatusMessage(components.MessageInfo, "cronjob", "Verificando status dos CronJobs...")
	}
}

// Tipo de mensagem para tick timer
type tickMsg time.Time