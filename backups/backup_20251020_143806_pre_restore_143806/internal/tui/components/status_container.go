package components

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"k8s-hpa-manager/internal/ui"
)

// MessageType define o tipo da mensagem para ícones e cores
type MessageType string

const (
	MessageSuccess MessageType = "success"
	MessageError   MessageType = "error"
	MessageWarning MessageType = "warning"
	MessageInfo    MessageType = "info"
	MessageDebug   MessageType = "debug"
)

// Message representa uma mensagem no container de status
type Message struct {
	Type      MessageType
	Source    string
	Content   string
	Timestamp time.Time
}

// StatusContainer é um container reutilizável para painéis de status
// As bordas são os limites visuais do container
type StatusContainer struct {
	width        int                        // Largura total incluindo bordas
	height       int                        // Altura total incluindo bordas
	title        string                     // Título do container
	messages     []Message                  // Mensagens do container
	scrollPos    int                        // Posição de scroll
	progressBars map[string]*ui.ProgressBar // Barras de progresso ativas
}

// NewStatusContainer cria um novo container de status com dimensões fixas
// As bordas fazem parte do container e marcam seus limites visuais
func NewStatusContainer(width, height int, title string) *StatusContainer {
	return &StatusContainer{
		width:        width,
		height:       height,
		title:        title,
		messages:     make([]Message, 0),
		scrollPos:    0,
		progressBars: make(map[string]*ui.ProgressBar),
	}
}

// AddMessage adiciona uma mensagem ao container
func (sc *StatusContainer) AddMessage(msgType MessageType, source, content string) {
	msg := Message{
		Type:      msgType,
		Source:    source,
		Content:   content,
		Timestamp: time.Now(),
	}
	sc.messages = append(sc.messages, msg)

	// Auto-scroll para mostrar mensagem mais recente
	sc.autoScroll()
}

// Métodos de conveniência para adicionar mensagens
func (sc *StatusContainer) AddSuccess(source, content string) {
	sc.AddMessage(MessageSuccess, source, content)
}

func (sc *StatusContainer) AddError(source, content string) {
	sc.AddMessage(MessageError, source, content)
}

func (sc *StatusContainer) AddWarning(source, content string) {
	sc.AddMessage(MessageWarning, source, content)
}

func (sc *StatusContainer) AddInfo(source, content string) {
	sc.AddMessage(MessageInfo, source, content)
}

func (sc *StatusContainer) AddDebug(source, content string) {
	sc.AddMessage(MessageDebug, source, content)
}

// Clear limpa todas as mensagens
func (sc *StatusContainer) Clear() {
	sc.messages = make([]Message, 0)
	sc.scrollPos = 0
}

// AddProgressBar adiciona uma nova barra de progresso
func (sc *StatusContainer) AddProgressBar(id, title string, total int) {
	pb := ui.NewProgressBar(id, title, total)
	// Usar estilo Rich Python com caracteres finos (━ e ╌)
	pb.Style = ui.DefaultProgressBarStyle() // Usa ━ e ╌ em vez de █ e ░
	pb.Style.Width = 30 // Largura reduzida da barra visual para caber em 1 linha (30 + % + título ≈ 130)
	pb.Style.ShowTime = false // Não mostrar tempo para economizar espaço
	pb.Style.ShowPercent = true
	sc.progressBars[id] = pb

	// DEBUG
	f, _ := os.OpenFile("/tmp/progress_debug.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("[%s] AddProgressBar: id=%s, title=%s, total=%d, status=%s\n",
			time.Now().Format("15:04:05"), id, title, total, pb.Status))
	}
}

// UpdateProgress atualiza o progresso de uma barra existente
func (sc *StatusContainer) UpdateProgress(id string, percentage int, statusText string) {
	if pb, exists := sc.progressBars[id]; exists {
		pb.Update(percentage, "running")
		pb.Title = statusText
	}
}

// CompleteProgress marca uma barra como completa
func (sc *StatusContainer) CompleteProgress(id string) {
	if pb, exists := sc.progressBars[id]; exists {
		pb.Complete()
		// Remover após 3 segundos
		go func() {
			time.Sleep(3 * time.Second)
			delete(sc.progressBars, id)
		}()
	}
}

// RemoveProgress remove uma barra de progresso
func (sc *StatusContainer) RemoveProgress(id string) {
	delete(sc.progressBars, id)
}

// ClearProgressBars limpa todas as barras de progresso
func (sc *StatusContainer) ClearProgressBars() {
	sc.progressBars = make(map[string]*ui.ProgressBar)
}

// Render renderiza o container completo com bordas como limites visuais
func (sc *StatusContainer) Render() string {
	// FORÇAR largura de 140 sempre
	sc.width = 140

	// Calcular padding para centralizar título
	// Total = ╭(1) + ─...─ + espaço(1) + título + espaço(1) + ─...─ + ╮(1)
	// Espaço para traços = width - 2(bordas) - 2(espaços) - largura_visual(título)
	titleWidth := runewidth.StringWidth(sc.title) // ✅ conta emojis como 2 células
	totalDashes := sc.width - titleWidth - 4 // -2 bordas -2 espaços ao redor do título
	if totalDashes < 0 {
		totalDashes = 0
	}
	leftDashes := totalDashes / 2
	rightDashes := totalDashes - leftDashes // garante que soma exata

	// Bordas do container (limites visuais)
	topBorder := "╭" + strings.Repeat("─", leftDashes) + " " + sc.title + " " + strings.Repeat("─", rightDashes) + "╮"
	bottomBorder := "╰" + strings.Repeat("─", sc.width-2) + "╯"

	// Processar mensagens visíveis baseado no scroll
	visibleMessages := sc.getVisibleMessages()
	contentLines := sc.renderMessages(visibleMessages)

	// Preencher linhas vazias até altura desejada
	// Linha vazia deve ter EXATAMENTE sc.width colunas visuais
	innerWidth := sc.width - 4
	emptyLine := "│ " + strings.Repeat(" ", innerWidth) + " │"

	// Verificar largura visual da linha vazia
	emptyLineWidth := runewidth.StringWidth(emptyLine)
	if emptyLineWidth != sc.width {
		// Ajustar se necessário
		diff := sc.width - emptyLineWidth
		emptyLine = "│ " + strings.Repeat(" ", innerWidth+diff) + " │"
	}

	desiredContentLines := sc.height - 2 // -2 para bordas superior/inferior

	for len(contentLines) < desiredContentLines {
		contentLines = append(contentLines, emptyLine)
	}

	// Montar container final com bordas como limites
	var result []string
	result = append(result, topBorder)
	result = append(result, contentLines...)
	result = append(result, bottomBorder)

	// VALIDAÇÃO FINAL: Verificar todas as linhas
	// DEBUG: salvar em arquivo
	debugFile := "/tmp/status_container_debug.txt"
	f, _ := os.OpenFile(debugFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("\n=== Render %s ===\n", time.Now().Format("15:04:05")))
		f.WriteString(fmt.Sprintf("sc.width configurado: %d\n", sc.width))
		for i, line := range result {
			lineWidth := runewidth.StringWidth(line)
			f.WriteString(fmt.Sprintf("Linha %d: largura visual=%d (esperado=%d) diff=%d\n", i, lineWidth, sc.width, sc.width-lineWidth))
			if lineWidth != sc.width {
				f.WriteString(fmt.Sprintf("  PROBLEMA: '%s'\n", line))
			}
		}
	}

	return strings.Join(result, "\n")
}

// getVisibleMessages obtém mensagens visíveis baseado no scroll
func (sc *StatusContainer) getVisibleMessages() []Message {
	totalMessages := len(sc.messages)
	if totalMessages == 0 {
		return []Message{}
	}

	maxVisibleLines := sc.height - 2 // -2 para bordas
	if totalMessages <= maxVisibleLines {
		return sc.messages
	}

	// Aplicar scroll
	start := sc.scrollPos
	end := start + maxVisibleLines
	if end > totalMessages {
		end = totalMessages
	}

	return sc.messages[start:end]
}

// renderMessages renderiza lista de mensagens e progress bars
func (sc *StatusContainer) renderMessages(messages []Message) []string {
	var lines []string
	var progressLines []string
	// Largura interna: width total - 2 (bordas │) - 2 (espaços internos)
	innerWidth := sc.width - 4

	// DEBUG
	f, _ := os.OpenFile("/tmp/progress_debug.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		f.WriteString(fmt.Sprintf("[%s] renderMessages: total progressBars=%d\n",
			time.Now().Format("15:04:05"), len(sc.progressBars)))
	}

	// Renderizar progress bars (incluindo completed até serem removidas)
	for id, pb := range sc.progressBars {
		if f != nil {
			f.WriteString(fmt.Sprintf("  - Progress bar %s: status=%s, percentage=%.0f%%\n",
				id, pb.Status, pb.GetPercentage()))
		}

		// Mostrar todas as progress bars (running, paused, completed)
		if pb.Status == "running" || pb.Status == "paused" || pb.Status == "completed" {
			progressLine := pb.Render()
			// Remover variation selectors
			progressLine = removeVariationSelectors(progressLine)

			if f != nil {
				f.WriteString(fmt.Sprintf("    Rendered line: '%s' (len=%d)\n", progressLine, runewidth.StringWidth(progressLine)))
			}

			// NÃO quebrar linha - progress bar deve caber em 1 linha
			progressLines = append(progressLines, sc.formatLineWithBorders(progressLine, innerWidth))
		}
	}

	if f != nil {
		f.WriteString(fmt.Sprintf("  Total progress lines: %d\n", len(progressLines)))
	}

	// Primeiro adicionar mensagens
	for _, msg := range messages {
		// Formatar mensagem
		formattedMsg := sc.formatMessage(msg)

		// Quebrar linha se muito longa (ao invés de truncar)
		wrappedLines := sc.wrapText(formattedMsg, innerWidth)

		for _, line := range wrappedLines {
			lines = append(lines, sc.formatLineWithBorders(line, innerWidth))
		}
	}

	// Depois inserir progress bars NO FINAL (novos itens aparecem em baixo)
	lines = append(lines, progressLines...)

	if f != nil {
		f.WriteString(fmt.Sprintf("  Total lines (messages + progress): %d\n", len(lines)))
		f.Close()
	}

	return lines
}

// removeVariationSelectors remove variation selectors invisíveis (U+FE0F, U+FE0E)
func removeVariationSelectors(s string) string {
	runes := []rune(s)
	result := make([]rune, 0, len(runes))

	for _, r := range runes {
		// Pular variation selectors
		if r == '\uFE0F' || r == '\uFE0E' {
			continue
		}
		result = append(result, r)
	}

	return string(result)
}

// formatMessage formata uma mensagem para exibição
func (sc *StatusContainer) formatMessage(msg Message) string {
	// Ícone baseado no tipo
	icon := sc.getMessageIcon(msg.Type)

	// Timestamp simples
	timeStr := msg.Timestamp.Format("15:04:05")

	// Formato: [15:04:05] 🔵 source: conteúdo
	formatted := fmt.Sprintf("[%s] %s %s: %s", timeStr, icon, msg.Source, msg.Content)

	// Remover variation selectors invisíveis que causam problemas de largura
	return removeVariationSelectors(formatted)
}

// getMessageIcon obtém ícone baseado no tipo da mensagem
func (sc *StatusContainer) getMessageIcon(msgType MessageType) string {
	switch msgType {
	case MessageSuccess:
		return "✅"
	case MessageError:
		return "❌"
	case MessageWarning:
		return "⚠️"
	case MessageDebug:
		return "🔍"
	default: // MessageInfo
		return "ℹ️"
	}
}

// autoScroll ajusta scroll para mostrar mensagens mais recentes
func (sc *StatusContainer) autoScroll() {
	totalMessages := len(sc.messages)
	maxVisibleLines := sc.height - 2 // -2 para bordas

	if totalMessages > maxVisibleLines {
		sc.scrollPos = totalMessages - maxVisibleLines
	}
}

// ScrollUp move scroll para cima
func (sc *StatusContainer) ScrollUp() {
	if sc.scrollPos > 0 {
		sc.scrollPos--
	}
}

// ScrollDown move scroll para baixo
func (sc *StatusContainer) ScrollDown() {
	totalMessages := len(sc.messages)
	maxVisibleLines := sc.height - 2
	maxScrollPos := totalMessages - maxVisibleLines

	if sc.scrollPos < maxScrollPos {
		sc.scrollPos++
	}
}

// wrapText quebra texto longo em múltiplas linhas sem cortar palavras
// Usa largura visual (emojis contam como 2 células)
func (sc *StatusContainer) wrapText(text string, maxWidth int) []string {
	// Verifica largura visual ao invés de contagem de runes
	if runewidth.StringWidth(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	runes := []rune(text)
	start := 0

	for start < len(runes) {
		// Encontra o ponto de corte baseado na largura visual
		currentWidth := 0
		end := start

		for end < len(runes) {
			charWidth := runewidth.RuneWidth(runes[end])
			if currentWidth+charWidth > maxWidth {
				break
			}
			currentWidth += charWidth
			end++
		}

		// Se não conseguiu adicionar nenhum caractere, força pelo menos 1
		if end == start {
			end = start + 1
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

		line := string(runes[start:end])
		lines = append(lines, line)
		start = end

		// Pula espaços no início da próxima linha
		for start < len(runes) && runes[start] == ' ' {
			start++
		}
	}

	return lines
}

// formatLineWithBorders formata uma linha com bordas e padding para ter exatamente sc.width colunas visuais
func (sc *StatusContainer) formatLineWithBorders(line string, innerWidth int) string {
	// Começar com a estrutura básica
	var result strings.Builder
	result.WriteString("│ ") // Borda esquerda + espaço

	// Adicionar conteúdo e calcular quantos espaços faltam
	contentWidth := runewidth.StringWidth(line)
	result.WriteString(line)

	// Preencher com espaços até completar innerWidth
	spacesNeeded := innerWidth - contentWidth
	if spacesNeeded > 0 {
		result.WriteString(strings.Repeat(" ", spacesNeeded))
	}

	result.WriteString(" │") // Espaço + borda direita

	paddedLine := result.String()

	// GARANTIR que a linha tem EXATAMENTE sc.width colunas visuais
	actualWidth := runewidth.StringWidth(paddedLine)
	if actualWidth != sc.width {
		// Ajustar se necessário
		diff := sc.width - actualWidth
		if diff > 0 {
			// Faltam espaços, adicionar antes da borda direita
			paddedLine = "│ " + line + strings.Repeat(" ", innerWidth-contentWidth+diff) + " │"
		} else {
			// Sobram caracteres, truncar conteúdo
			truncWidth := innerWidth + diff - 2 // -2 para margens
			if truncWidth < 0 {
				truncWidth = 0
			}
			truncLine := runewidth.Truncate(line, truncWidth, "")
			paddedLine = "│ " + truncLine + strings.Repeat(" ", innerWidth-runewidth.StringWidth(truncLine)) + " │"
		}
	}

	return paddedLine
}