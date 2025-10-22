package layout

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ResponsivePanel representa um painel com capacidades responsivas
type ResponsivePanel struct {
	Title       string
	Content     []string
	ScrollOffset int
	MaxWidth    int
	Color       lipgloss.Color
	layoutMgr   *LayoutManager
}

// NewResponsivePanel cria um novo painel responsivo
func NewResponsivePanel(title string, content []string, color lipgloss.Color, layoutMgr *LayoutManager) *ResponsivePanel {
	return &ResponsivePanel{
		Title:        title,
		Content:      content,
		ScrollOffset: 0,
		MaxWidth:     layoutMgr.StandardPanel.MinWidth + 20, // 70+20=90 máximo responsivo
		Color:        color,
		layoutMgr:    layoutMgr,
	}
}

// Render renderiza o painel com todas as funcionalidades responsivas
func (rp *ResponsivePanel) Render() string {
	// Começar com largura padrão 70 e ajustar se necessário
	standardWidth := rp.layoutMgr.StandardPanel.MinWidth
	contentWidth := rp.calculateContentWidth()

	// Usar largura padrão como base, só aumentar se conteúdo for muito largo
	maxWidth := standardWidth
	if contentWidth+6 > standardWidth { // +6 para bordas e padding
		maxWidth = contentWidth + 6
		if maxWidth > rp.MaxWidth {
			maxWidth = rp.MaxWidth
		}
	}

	// Calcular altura responsiva
	totalLines := len(rp.Content)
	panelHeight := rp.layoutMgr.CalculateResponsivePanelHeight(totalLines)

	// Aplicar scroll se necessário
	displayContent, scrollInfo := rp.applyScroll(panelHeight)

	// Título com informação de scroll
	title := rp.Title
	if scrollInfo != "" {
		title += scrollInfo
	}

	return rp.renderPanelWithTitle(displayContent, title, maxWidth, panelHeight)
}

// GetActualHeight retorna a altura real do painel (para cálculo de espaçamento)
func (rp *ResponsivePanel) GetActualHeight() int {
	totalLines := len(rp.Content)
	return rp.layoutMgr.CalculateResponsivePanelHeight(totalLines)
}

// ScrollUp move o scroll para cima
func (rp *ResponsivePanel) ScrollUp() {
	if rp.ScrollOffset > 0 {
		rp.ScrollOffset--
	}
}

// ScrollDown move o scroll para baixo
func (rp *ResponsivePanel) ScrollDown() {
	totalLines := len(rp.Content)
	panelHeight := rp.layoutMgr.CalculateResponsivePanelHeight(totalLines)
	visibleLines := rp.layoutMgr.CalculateVisibleLines(panelHeight)

	maxOffset := totalLines - visibleLines
	if maxOffset > 0 && rp.ScrollOffset < maxOffset {
		rp.ScrollOffset++
	}
}

// AdjustScrollToKeepItemVisible ajusta o scroll para manter um item específico visível
func (rp *ResponsivePanel) AdjustScrollToKeepItemVisible(itemLinePosition int) {
	totalLines := len(rp.Content)
	panelHeight := rp.layoutMgr.CalculateResponsivePanelHeight(totalLines)
	visibleLines := rp.layoutMgr.CalculateVisibleLines(panelHeight)

	// Se o item está antes da janela visível
	if itemLinePosition < rp.ScrollOffset {
		rp.ScrollOffset = itemLinePosition
	}

	// Se o item está depois da janela visível
	if itemLinePosition >= rp.ScrollOffset+visibleLines {
		rp.ScrollOffset = itemLinePosition - visibleLines + 1
	}

	// Garantir limites
	if rp.ScrollOffset < 0 {
		rp.ScrollOffset = 0
	}

	maxOffset := totalLines - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if rp.ScrollOffset > maxOffset {
		rp.ScrollOffset = maxOffset
	}
}

// calculateContentWidth calcula a largura baseada no conteúdo
func (rp *ResponsivePanel) calculateContentWidth() int {
	contentWidth := 0
	for _, line := range rp.Content {
		// Remover códigos de cor/estilo para calcular largura real
		cleanLine := lipgloss.NewStyle().UnsetBackground().UnsetForeground().Render(line)
		lineWidth := len([]rune(cleanLine))
		if lineWidth > contentWidth {
			contentWidth = lineWidth
		}
	}
	return contentWidth
}

// applyScroll aplica o scroll e retorna conteúdo visível e informação de scroll
func (rp *ResponsivePanel) applyScroll(panelHeight int) (string, string) {
	totalLines := len(rp.Content)
	visibleLines := rp.layoutMgr.CalculateVisibleLines(panelHeight)

	var displayLines []string
	var scrollInfo string

	if rp.layoutMgr.IsScrollNeeded(totalLines) {
		// Scroll necessário
		start := rp.ScrollOffset
		end := start + visibleLines
		if end > totalLines {
			end = totalLines
		}

		displayLines = rp.Content[start:end]
		scrollInfo = fmt.Sprintf(" [%d-%d/%d]", start+1, end, totalLines)
	} else {
		// Tudo cabe
		displayLines = rp.Content
		rp.ScrollOffset = 0
	}

	return strings.Join(displayLines, "\n"), scrollInfo
}

// renderPanelWithTitle renderiza um painel com título integrado na borda
func (rp *ResponsivePanel) renderPanelWithTitle(content, title string, width, height int) string {
	// Estilo base para o painel
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rp.Color).
		Width(width - 2). // -2 para compensar as bordas
		Height(height - 2). // -2 para compensar as bordas
		Padding(0, 1)

	// Renderizar conteúdo no painel
	renderedContent := panelStyle.Render(content)

	// Modificar a primeira linha para incluir o título
	lines := strings.Split(renderedContent, "\n")
	if len(lines) > 0 {
		firstLine := lines[0]

		// Calcular posição do título
		titleWithSpacing := fmt.Sprintf(" %s ", title)
		titleLen := len([]rune(titleWithSpacing))

		// Obter runes da primeira linha para manipulação Unicode-safe
		firstLineRunes := []rune(firstLine)

		if len(firstLineRunes) >= titleLen+4 { // +4 para margem
			// Posição centralizada
			startPos := (len(firstLineRunes) - titleLen) / 2

			// Substituir parte da linha com o título
			titleRunes := []rune(titleWithSpacing)
			copy(firstLineRunes[startPos:startPos+titleLen], titleRunes)

			lines[0] = string(firstLineRunes)
		}
	}

	return strings.Join(lines, "\n")
}

// FixedPanel representa um painel com dimensões fixas (como Status)
type FixedPanel struct {
	Title   string
	Content string
	Width   int
	Height  int
	Color   lipgloss.Color
}

// NewFixedPanel cria um novo painel fixo
func NewFixedPanel(title, content string, color lipgloss.Color, layoutMgr *LayoutManager) *FixedPanel {
	width, height := layoutMgr.GetStatusPanelDimensions()
	return &FixedPanel{
		Title:   title,
		Content: content,
		Width:   width,
		Height:  height,
		Color:   color,
	}
}

// Render renderiza o painel fixo
func (fp *FixedPanel) Render() string {
	return fp.renderPanelWithTitle(fp.Content, fp.Title, fp.Width, fp.Height)
}

// GetActualHeight retorna a altura do painel fixo
func (fp *FixedPanel) GetActualHeight() int {
	return fp.Height
}

// renderPanelWithTitle para painel fixo
func (fp *FixedPanel) renderPanelWithTitle(content, title string, width, height int) string {
	// Mesmo método que ResponsivePanel, mas sem scroll
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fp.Color).
		Width(width - 2).
		Height(height - 2).
		Padding(0, 1)

	renderedContent := panelStyle.Render(content)
	lines := strings.Split(renderedContent, "\n")

	if len(lines) > 0 {
		firstLine := lines[0]
		titleWithSpacing := fmt.Sprintf(" %s ", title)
		titleLen := len([]rune(titleWithSpacing))
		firstLineRunes := []rune(firstLine)

		if len(firstLineRunes) >= titleLen+4 {
			startPos := (len(firstLineRunes) - titleLen) / 2
			titleRunes := []rune(titleWithSpacing)
			copy(firstLineRunes[startPos:startPos+titleLen], titleRunes)
			lines[0] = string(firstLineRunes)
		}
	}

	return strings.Join(lines, "\n")
}