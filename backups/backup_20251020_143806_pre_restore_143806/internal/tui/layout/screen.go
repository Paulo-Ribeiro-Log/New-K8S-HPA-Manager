package layout

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TwoColumnLayout representa um layout de duas colunas com painel status inferior
type TwoColumnLayout struct {
	layoutMgr *LayoutManager
}

// NewTwoColumnLayout cria um novo layout de duas colunas
func NewTwoColumnLayout(layoutMgr *LayoutManager) *TwoColumnLayout {
	return &TwoColumnLayout{
		layoutMgr: layoutMgr,
	}
}

// Render renderiza o layout completo com dois painéis superiores e um inferior
func (tcl *TwoColumnLayout) Render(leftPanel, rightPanel, statusPanel string, leftHeight, rightHeight int) string {
	var content strings.Builder

	// Calcular espaçamento dinâmico
	spacing := tcl.layoutMgr.CalculateSpacing(leftHeight, rightHeight)

	// Adicionar espaçamento aos painéis
	leftPanelWithSpacing := leftPanel + spacing
	rightPanelWithSpacing := rightPanel + spacing

	// Combinar painéis horizontalmente
	content.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanelWithSpacing, rightPanelWithSpacing))

	// Adicionar painel status
	content.WriteString("\n")
	content.WriteString(statusPanel)

	return content.String()
}

// SingleColumnLayout representa um layout de uma coluna com painel status inferior
type SingleColumnLayout struct {
	layoutMgr *LayoutManager
}

// NewSingleColumnLayout cria um novo layout de uma coluna
func NewSingleColumnLayout(layoutMgr *LayoutManager) *SingleColumnLayout {
	return &SingleColumnLayout{
		layoutMgr: layoutMgr,
	}
}

// Render renderiza o layout de uma coluna
func (scl *SingleColumnLayout) Render(mainPanel, statusPanel string, mainHeight int) string {
	var content strings.Builder

	// Calcular espaçamento (usando altura mínima para o segundo painel inexistente)
	minHeight := scl.layoutMgr.StandardPanel.MinHeight + 2
	spacing := scl.layoutMgr.CalculateSpacing(mainHeight, minHeight)

	// Adicionar painel principal com espaçamento
	content.WriteString(mainPanel)
	content.WriteString(spacing)

	// Adicionar painel status
	content.WriteString("\n")
	content.WriteString(statusPanel)

	return content.String()
}

// LayoutBuilder facilita a construção de layouts complexos
type LayoutBuilder struct {
	layoutMgr    *LayoutManager
	sessionInfo  string
	helpText     string
	panels       []string
	panelHeights []int
}

// NewLayoutBuilder cria um novo construtor de layout
func NewLayoutBuilder(layoutMgr *LayoutManager) *LayoutBuilder {
	return &LayoutBuilder{
		layoutMgr:    layoutMgr,
		panels:       make([]string, 0),
		panelHeights: make([]int, 0),
	}
}

// SetSessionInfo define as informações da sessão (header)
func (lb *LayoutBuilder) SetSessionInfo(sessionInfo string) *LayoutBuilder {
	lb.sessionInfo = sessionInfo
	return lb
}

// SetHelpText define o texto de ajuda (footer)
func (lb *LayoutBuilder) SetHelpText(helpText string) *LayoutBuilder {
	lb.helpText = helpText
	return lb
}

// AddPanel adiciona um painel ao layout
func (lb *LayoutBuilder) AddPanel(panel string, height int) *LayoutBuilder {
	lb.panels = append(lb.panels, panel)
	lb.panelHeights = append(lb.panelHeights, height)
	return lb
}

// BuildTwoColumn constrói um layout de duas colunas
func (lb *LayoutBuilder) BuildTwoColumn(statusPanel string) string {
	if len(lb.panels) < 2 {
		panic("TwoColumn layout requires at least 2 panels")
	}

	var content strings.Builder

	// Session info (header)
	if lb.sessionInfo != "" {
		content.WriteString(lb.sessionInfo)
	}

	// Layout principal
	layout := NewTwoColumnLayout(lb.layoutMgr)
	mainContent := layout.Render(
		lb.panels[0], lb.panels[1], statusPanel,
		lb.panelHeights[0], lb.panelHeights[1],
	)
	content.WriteString(mainContent)

	// Help text (footer)
	if lb.helpText != "" {
		content.WriteString("\n")
		content.WriteString(lb.helpText)
	}

	return content.String()
}

// BuildSingleColumn constrói um layout de uma coluna
func (lb *LayoutBuilder) BuildSingleColumn(statusPanel string) string {
	if len(lb.panels) < 1 {
		panic("SingleColumn layout requires at least 1 panel")
	}

	var content strings.Builder

	// Session info (header)
	if lb.sessionInfo != "" {
		content.WriteString(lb.sessionInfo)
	}

	// Layout principal
	layout := NewSingleColumnLayout(lb.layoutMgr)
	mainContent := layout.Render(
		lb.panels[0], statusPanel,
		lb.panelHeights[0],
	)
	content.WriteString(mainContent)

	// Help text (footer)
	if lb.helpText != "" {
		content.WriteString("\n")
		content.WriteString(lb.helpText)
	}

	return content.String()
}

// BuildCustom permite layouts customizados
func (lb *LayoutBuilder) BuildCustom(customRenderer func(panels []string, heights []int, statusPanel string) string, statusPanel string) string {
	var content strings.Builder

	// Session info (header)
	if lb.sessionInfo != "" {
		content.WriteString(lb.sessionInfo)
	}

	// Layout customizado
	mainContent := customRenderer(lb.panels, lb.panelHeights, statusPanel)
	content.WriteString(mainContent)

	// Help text (footer)
	if lb.helpText != "" {
		content.WriteString("\n")
		content.WriteString(lb.helpText)
	}

	return content.String()
}