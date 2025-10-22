package layout

import "github.com/charmbracelet/lipgloss"

// Cores padrão do projeto
var (
	PrimaryColor   = lipgloss.Color("#00ADD8") // Azul Go
	SuccessColor   = lipgloss.Color("#22C55E") // Verde sucesso
	WarningColor   = lipgloss.Color("#F59E0B") // Amarelo warning
	ErrorColor     = lipgloss.Color("#EF4444") // Vermelho erro
	SecondaryColor = lipgloss.Color("#6B7280") // Cinza secundário
)

// Estilos padrão para elementos comuns
var (
	TitleStyle = lipgloss.NewStyle().
		Foreground(PrimaryColor).
		Bold(true).
		Margin(0, 0, 1, 0)

	HelpStyle = lipgloss.NewStyle().
		Foreground(SecondaryColor).
		Margin(1, 0, 0, 0)

	SelectedItemStyle = lipgloss.NewStyle().
		Background(PrimaryColor).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1)

	NormalItemStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))
)

// Dimensões padrão do projeto
const (
	// Dimensões adaptativas - interface responsiva se ajusta ao terminal do usuário
	MinTerminalWidth  = 80  // Largura padrão de terminal (compatível com 80 colunas)
	MinTerminalHeight = 24  // Altura padrão de terminal (compatível com 24 linhas)

	// Painéis padrão
	StandardPanelMinHeight = 18  // Altura mínima (sem bordas)
	StandardPanelMaxHeight = 35  // Altura máxima (com scroll)

	// Espaçamento
	BaseSpacingLines = 20 // Espaçamento base entre painéis superiores e status

	// Bordas
	BorderLines = 2 // Linhas adicionadas pelas bordas

	// Scroll
	MinScrollOffset = 0 // Offset mínimo de scroll
)

// PanelType define os tipos de painéis disponíveis
type PanelType int

const (
	StandardPanel PanelType = iota
	StatusPanel
	CustomPanel
)

// ScrollDirection define as direções de scroll
type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollPageUp
	ScrollPageDown
	ScrollHome
	ScrollEnd
)

// LayoutType define os tipos de layout disponíveis
type LayoutType int

const (
	TwoColumnLayoutType LayoutType = iota
	SingleColumnLayoutType
	ThreeColumnLayoutType
	CustomLayoutType
)

// ResponsiveConfig configurações para comportamento responsivo
type ResponsiveConfig struct {
	EnableAutoScroll     bool // Auto-scroll para manter item selecionado visível
	EnableMouseScroll    bool // Suporte a scroll com mouse
	EnableShiftScroll    bool // Suporte a Shift+Up/Down para scroll manual
	ScrollSensitivity    int  // Linhas por movimento de scroll
	AutoScrollMargin     int  // Margem em linhas para auto-scroll
	ExpandOnContentGrow  bool // Expandir painel quando conteúdo cresce
	ContractOnContentShrink bool // Contrair painel quando conteúdo diminui
}

// DefaultResponsiveConfig retorna a configuração responsiva padrão
func DefaultResponsiveConfig() ResponsiveConfig {
	return ResponsiveConfig{
		EnableAutoScroll:        true,
		EnableMouseScroll:       true,
		EnableShiftScroll:       true,
		ScrollSensitivity:       1,
		AutoScrollMargin:        2,
		ExpandOnContentGrow:     true,
		ContractOnContentShrink: true,
	}
}

// CalculateTotalHeight calcula a altura total de um painel (conteúdo + bordas)
func CalculateTotalHeight(contentHeight int) int {
	return contentHeight + BorderLines
}

// CalculateContentHeight calcula a altura de conteúdo de um painel (total - bordas)
func CalculateContentHeight(totalHeight int) int {
	height := totalHeight - BorderLines
	if height < 1 {
		height = 1
	}
	return height
}

// ClampHeight limita uma altura entre valores mínimo e máximo
func ClampHeight(height, min, max int) int {
	if height < min {
		return min
	}
	if height > max {
		return max
	}
	return height
}

// ClampWidth limita uma largura entre valores mínimo e máximo
func ClampWidth(width, min, max int) int {
	if width < min {
		return min
	}
	if width > max {
		return max
	}
	return width
}