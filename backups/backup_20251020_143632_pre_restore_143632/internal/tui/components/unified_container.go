package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"k8s-hpa-manager/internal/models"
)

// UnifiedContainer representa o container principal 200x50 com moldura, header e status container
type UnifiedContainer struct {
	width           int
	height          int
	title           string
	content         string
	headerText      string
	statusContainer *StatusContainer
	showStatus      bool
}

// Dimensões fixas do container
const (
	CONTAINER_WIDTH    = 200
	CONTAINER_HEIGHT   = 50
	STATUS_HEIGHT      = 6   // Altura do status container
	STATUS_WIDTH       = 140 // Largura do status container FIXA em 140 colunas
	MAIN_CONTENT_HEIGHT = CONTAINER_HEIGHT - STATUS_HEIGHT - 3 // Altura disponível para conteúdo principal
)

// Estilos para o container unificado
var (
	// Estilo da moldura principal
	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#007ACC")).
			Width(CONTAINER_WIDTH).
			Height(CONTAINER_HEIGHT)

	// Estilo do header
	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#007ACC")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1).
			Margin(0, 0, 1, 0).
			Width(CONTAINER_WIDTH - 4). // -4 para bordas
			Align(lipgloss.Center)       // Centralizar texto

	// Estilo do conteúdo
	contentStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Width(CONTAINER_WIDTH - 6).  // -6 para bordas e padding
			Height(CONTAINER_HEIGHT - 6) // -6 para header e bordas
)

// NewUnifiedContainer cria um novo container unificado com status container integrado
func NewUnifiedContainer() *UnifiedContainer {
	// Criar o status container integrado
	statusContainer := NewStatusContainer(STATUS_WIDTH, STATUS_HEIGHT, "📊 Status e Informações")

	return &UnifiedContainer{
		width:           CONTAINER_WIDTH,
		height:          CONTAINER_HEIGHT,
		statusContainer: statusContainer,
		showStatus:      true,
	}
}

// SetTitle define o título do container baseado na função atual
func (uc *UnifiedContainer) SetTitle(currentState models.AppState) {
	switch currentState {
	case models.StateClusterSelection:
		uc.headerText = "K8s HPA Manager - Seleção de Clusters"
	case models.StateSessionSelection:
		uc.headerText = "K8s HPA Manager - Gerenciamento de Sessões"
	case models.StateSessionFolderSelection:
		uc.headerText = "K8s HPA Manager - Seleção de Pastas"
	case models.StateNamespaceSelection:
		uc.headerText = "K8s HPA Manager - Seleção de Namespaces"
	case models.StateHPASelection:
		uc.headerText = "K8s HPA Manager - Gerenciamento de HPAs"
	case models.StateHPAEditing:
		uc.headerText = "K8s HPA Manager - Editando HPA"
	case models.StateNodeSelection:
		uc.headerText = "K8s HPA Manager - Gerenciamento de Node Pools"
	case models.StateNodeEditing:
		uc.headerText = "K8s HPA Manager - Editando Node Pool"
	case models.StateCronJobSelection:
		uc.headerText = "K8s HPA Manager - Gerenciamento de CronJobs"
	case models.StateCronJobEditing:
		uc.headerText = "K8s HPA Manager - Editando CronJob"
	case models.StateAddingCluster:
		uc.headerText = "K8s HPA Manager - Adicionando Cluster"
	case models.StateHelp:
		uc.headerText = "K8s HPA Manager - Sistema de Ajuda"
	default:
		uc.headerText = "K8s HPA Manager - Interface Principal"
	}
}

// SetContent define o conteúdo principal do container
func (uc *UnifiedContainer) SetContent(content string) {
	uc.content = content
}

// AddStatusMessage adiciona uma mensagem ao status container
func (uc *UnifiedContainer) AddStatusMessage(msgType MessageType, source, content string) {
	if uc.statusContainer != nil {
		uc.statusContainer.AddMessage(msgType, source, content)
	}
}

// SetShowStatus controla se o status container é exibido
func (uc *UnifiedContainer) SetShowStatus(show bool) {
	uc.showStatus = show
}

// GetStatusContainer retorna o status container para acesso direto
func (uc *UnifiedContainer) GetStatusContainer() *StatusContainer {
	return uc.statusContainer
}

// Render renderiza o container unificado completo com status integrado
func (uc *UnifiedContainer) Render() string {
	// Header com título da função atual
	header := headerStyle.Render(uc.headerText)

	// Processa o conteúdo principal para caber no espaço disponível
	processedContent := uc.processContent(uc.content)

	// Ajusta o estilo do conteúdo para a nova altura
	mainContentStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(CONTAINER_WIDTH - 6).
		Height(MAIN_CONTENT_HEIGHT)

	content := mainContentStyle.Render(processedContent)

	// Renderiza o status container se habilitado
	var containerContent string
	if uc.showStatus && uc.statusContainer != nil {
		// Renderiza o status container
		statusContent := uc.statusContainer.Render()

		// Centralizar o status container dentro do espaço disponível (200 colunas)
		// StatusContainer tem 140 colunas, então precisa de 30 espaços de cada lado
		leftPadding := (CONTAINER_WIDTH - STATUS_WIDTH) / 2
		centeredStatus := lipgloss.NewStyle().
			PaddingLeft(leftPadding).
			Render(statusContent)

		// Combina header + content + status centralizado
		containerContent = lipgloss.JoinVertical(lipgloss.Left,
			header,
			content,
			centeredStatus)
	} else {
		// Apenas header + content
		containerContent = lipgloss.JoinVertical(lipgloss.Left, header, content)
	}

	// Aplica a moldura principal
	return containerStyle.Render(containerContent)
}

// processContent processa o conteúdo para caber no espaço disponível
func (uc *UnifiedContainer) processContent(content string) string {
	lines := strings.Split(content, "\n")

	// Largura disponível para conteúdo (descontando padding e bordas)
	availableWidth := CONTAINER_WIDTH - 8 // -8 para bordas + padding

	// Altura disponível para conteúdo principal (descontando header, status e bordas)
	availableHeight := MAIN_CONTENT_HEIGHT - 2 // -2 para padding interno

	var processedLines []string

	// Processa cada linha com quebra inteligente
	for _, line := range lines {
		if len(line) == 0 {
			processedLines = append(processedLines, "")
			continue
		}

		// Quebra linhas longas
		if len([]rune(line)) <= availableWidth {
			processedLines = append(processedLines, line)
		} else {
			// Quebra a linha em pedaços
			runes := []rune(line)
			start := 0

			for start < len(runes) {
				end := start + availableWidth
				if end > len(runes) {
					end = len(runes)
				}

				// Tenta quebrar em espaço para não cortar palavras
				if end < len(runes) {
					for i := end - 1; i > start && i > end-20; i-- {
						if runes[i] == ' ' {
							end = i
							break
						}
					}
				}

				chunk := string(runes[start:end])
				processedLines = append(processedLines, chunk)
				start = end

				// Pula espaços no início da próxima linha
				for start < len(runes) && runes[start] == ' ' {
					start++
				}
			}
		}
	}

	// Limita a altura
	if len(processedLines) > availableHeight {
		processedLines = processedLines[:availableHeight]
		// Adiciona indicador de conteúdo truncado
		if availableHeight > 0 {
			processedLines[availableHeight-1] = strings.TrimSuffix(processedLines[availableHeight-1], "...") + "..."
		}
	}

	// Preenche com linhas vazias até a altura desejada
	for len(processedLines) < availableHeight {
		processedLines = append(processedLines, "")
	}

	return strings.Join(processedLines, "\n")
}

// GetDimensions retorna as dimensões do container
func (uc *UnifiedContainer) GetDimensions() (int, int) {
	return uc.width, uc.height
}

// RenderWithDebugInfo renderiza o container com informações de debug (opcional)
func (uc *UnifiedContainer) RenderWithDebugInfo(debugMode bool) string {
	result := uc.Render()

	if debugMode {
		debugInfo := fmt.Sprintf("Container: %dx%d | Header: %s",
			uc.width, uc.height, uc.headerText)
		debugStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)

		result += "\n" + debugStyle.Render(debugInfo)
	}

	return result
}