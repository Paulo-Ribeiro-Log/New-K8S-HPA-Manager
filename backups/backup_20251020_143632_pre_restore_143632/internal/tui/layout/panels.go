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
	lines := strings.Split(content, "\n")

	// Calcular largura necessária baseada no conteúdo
	maxLineLength := 0
	for _, line := range lines {
		realLength := len([]rune(line))
		if realLength > maxLineLength {
			maxLineLength = realLength
		}
	}

	// Garante largura mínima
	titleLength := len([]rune(title))
	neededWidth := titleLength + 6 // título + espaços + padding
	if maxLineLength+4 > neededWidth {
		neededWidth = maxLineLength + 4
	}
	if width > neededWidth {
		neededWidth = width
	}

	// Calcula padding para o título
	titlePadding := (neededWidth - titleLength - 6) / 2
	if titlePadding < 0 {
		titlePadding = 0
	}

	// Constrói bordas com título
	topBorder := "╭" + strings.Repeat("─", titlePadding+1) + " " + title + " " + strings.Repeat("─", neededWidth-titleLength-titlePadding-5) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", neededWidth-2) + "╯"

	// Constrói conteúdo
	var contentLines []string
	contentWidth := neededWidth - 4 // espaço interno

	for _, line := range lines {
		// Adiciona padding à direita para completar a largura
		paddedLine := "│ " + line
		lineDisplayWidth := lipgloss.Width(line)
		spacesNeeded := contentWidth - lineDisplayWidth
		if spacesNeeded > 0 {
			paddedLine += strings.Repeat(" ", spacesNeeded)
		}
		paddedLine += " │"
		contentLines = append(contentLines, paddedLine)
	}

	// Preenche altura se necessário (até o mínimo)
	emptyLine := "│" + strings.Repeat(" ", neededWidth-2) + "│"
	minContentLines := 5
	for len(contentLines) < minContentLines {
		contentLines = append(contentLines, emptyLine)
	}

	// Aplica cor na borda
	borderStyle := lipgloss.NewStyle().Foreground(rp.Color)
	styledTopBorder := borderStyle.Render(topBorder)

	// Aplica cor nas linhas de conteúdo (apenas nos caracteres │)
	var styledContentLines []string
	for _, line := range contentLines {
		if len(line) > 0 {
			runes := []rune(line)
			if len(runes) > 0 {
				firstChar := borderStyle.Render(string(runes[0]))
				lastChar := borderStyle.Render(string(runes[len(runes)-1]))
				middle := ""
				if len(runes) > 2 {
					middle = string(runes[1 : len(runes)-1])
				}
				styledLine := firstChar + middle + lastChar
				styledContentLines = append(styledContentLines, styledLine)
			}
		}
	}

	styledBottomBorder := borderStyle.Render(bottomBorder)

	// Monta painel completo
	result := styledTopBorder + "\n" + strings.Join(styledContentLines, "\n") + "\n" + styledBottomBorder
	return result
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
	lines := strings.Split(content, "\n")

	// Calcular largura necessária
	maxLineLength := 0
	for _, line := range lines {
		realLength := len([]rune(line))
		if realLength > maxLineLength {
			maxLineLength = realLength
		}
	}

	titleLength := len([]rune(title))
	neededWidth := titleLength + 6
	if maxLineLength+4 > neededWidth {
		neededWidth = maxLineLength + 4
	}
	if width > neededWidth {
		neededWidth = width
	}

	titlePadding := (neededWidth - titleLength - 6) / 2
	if titlePadding < 0 {
		titlePadding = 0
	}

	// Bordas com título
	topBorder := "╭" + strings.Repeat("─", titlePadding+1) + " " + title + " " + strings.Repeat("─", neededWidth-titleLength-titlePadding-5) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", neededWidth-2) + "╯"

	// Conteúdo
	var contentLines []string
	contentWidth := neededWidth - 4

	for _, line := range lines {
		paddedLine := "│ " + line
		lineDisplayWidth := lipgloss.Width(line)
		spacesNeeded := contentWidth - lineDisplayWidth
		if spacesNeeded > 0 {
			paddedLine += strings.Repeat(" ", spacesNeeded)
		}
		paddedLine += " │"
		contentLines = append(contentLines, paddedLine)
	}

	// Preenche altura
	emptyLine := "│" + strings.Repeat(" ", neededWidth-2) + "│"
	minContentLines := 5
	for len(contentLines) < minContentLines {
		contentLines = append(contentLines, emptyLine)
	}

	// Aplica cor
	borderStyle := lipgloss.NewStyle().Foreground(fp.Color)
	styledTopBorder := borderStyle.Render(topBorder)

	var styledContentLines []string
	for _, line := range contentLines {
		if len(line) > 0 {
			runes := []rune(line)
			if len(runes) > 0 {
				firstChar := borderStyle.Render(string(runes[0]))
				lastChar := borderStyle.Render(string(runes[len(runes)-1]))
				middle := ""
				if len(runes) > 2 {
					middle = string(runes[1 : len(runes)-1])
				}
				styledLine := firstChar + middle + lastChar
				styledContentLines = append(styledContentLines, styledLine)
			}
		}
	}

	styledBottomBorder := borderStyle.Render(bottomBorder)

	return styledTopBorder + "\n" + strings.Join(styledContentLines, "\n") + "\n" + styledBottomBorder
}