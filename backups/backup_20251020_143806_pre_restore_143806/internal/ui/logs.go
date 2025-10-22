package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// LogLevel define o nível de log
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelSuccess
)

// String retorna a representação string do nível
func (ll LogLevel) String() string {
	switch ll {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelSuccess:
		return "SUCCESS"
	default:
		return "UNKNOWN"
	}
}

// GetIcon retorna o ícone para o nível
func (ll LogLevel) GetIcon() string {
	switch ll {
	case LogLevelDebug:
		return "🔍"
	case LogLevelInfo:
		return "ℹ️"
	case LogLevelWarn:
		return "⚠️"
	case LogLevelError:
		return "❌"
	case LogLevelSuccess:
		return "✅"
	default:
		return "📝"
	}
}

// GetColor retorna a cor para o nível
func (ll LogLevel) GetColor() lipgloss.Color {
	switch ll {
	case LogLevelDebug:
		return lipgloss.Color("#6B7280") // Cinza
	case LogLevelInfo:
		return lipgloss.Color("#3B82F6") // Azul
	case LogLevelWarn:
		return lipgloss.Color("#F59E0B") // Amarelo
	case LogLevelError:
		return lipgloss.Color("#EF4444") // Vermelho
	case LogLevelSuccess:
		return lipgloss.Color("#10B981") // Verde
	default:
		return lipgloss.Color("#9CA3AF") // Cinza claro
	}
}

// LogEntry representa uma entrada de log
type LogEntry struct {
	ID        string
	Timestamp time.Time
	Level     LogLevel
	Source    string // Módulo que gerou o log (azure, k8s, session, etc.)
	Message   string
	Details   string // Detalhes adicionais (stack trace, etc.)
	Metadata  map[string]interface{}
}

// NewLogEntry cria uma nova entrada de log
func NewLogEntry(level LogLevel, source, message string) *LogEntry {
	return &LogEntry{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Message:   message,
		Metadata:  make(map[string]interface{}),
	}
}

// WithDetails adiciona detalhes à entrada
func (le *LogEntry) WithDetails(details string) *LogEntry {
	le.Details = details
	return le
}

// WithMetadata adiciona metadados
func (le *LogEntry) WithMetadata(key string, value interface{}) *LogEntry {
	le.Metadata[key] = value
	return le
}

// Render renderiza a entrada de log
func (le *LogEntry) Render(showTime bool, maxWidth int) string {
	// Estilo baseado no nível
	levelStyle := lipgloss.NewStyle().
		Foreground(le.Level.GetColor()).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	sourceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F3F4F6"))

	var parts []string

	// Timestamp (se solicitado)
	if showTime {
		timeStr := le.Timestamp.Format("15:04:05")
		parts = append(parts, timeStyle.Render(timeStr))
	}

	// Ícone e nível
	icon := le.Level.GetIcon()
	parts = append(parts, levelStyle.Render(fmt.Sprintf("%s %s", icon, le.Level.String())))

	// Source
	if le.Source != "" {
		parts = append(parts, sourceStyle.Render(fmt.Sprintf("[%s]", le.Source)))
	}

	// Construir primeira linha
	prefix := strings.Join(parts, " ")

	// Calcular espaço restante para mensagem
	messageSpace := maxWidth - lipgloss.Width(prefix) - 1
	if messageSpace < 20 {
		messageSpace = 20
	}

	// Quebrar mensagem se necessário
	message := le.Message
	if len(message) > messageSpace {
		message = message[:messageSpace-3] + "..."
	}

	line := fmt.Sprintf("%s %s", prefix, messageStyle.Render(message))

	// Adicionar detalhes em linha separada se houver
	if le.Details != "" {
		detailStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)

		details := le.Details
		if len(details) > maxWidth-4 {
			details = details[:maxWidth-7] + "..."
		}
		line += "\n" + detailStyle.Render("    "+details)
	}

	return line
}

// RenderCompact renderiza versão compacta
func (le *LogEntry) RenderCompact(maxWidth int) string {
	icon := le.Level.GetIcon()
	levelStyle := lipgloss.NewStyle().Foreground(le.Level.GetColor())
	messageStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6"))

	// Formato compacto: ícone + mensagem
	prefix := levelStyle.Render(icon)
	messageSpace := maxWidth - lipgloss.Width(prefix) - 1

	message := le.Message
	if len(message) > messageSpace {
		message = message[:messageSpace-3] + "..."
	}

	return fmt.Sprintf("%s %s", prefix, messageStyle.Render(message))
}

// RenderFullMessage renderiza mensagem completa sem truncamento, quebrando em múltiplas linhas
func (le *LogEntry) RenderFullMessage(maxWidth int, showTime bool) []string {
	// Estilo baseado no nível
	levelStyle := lipgloss.NewStyle().
		Foreground(le.Level.GetColor()).
		Bold(true)

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	sourceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F3F4F6"))

	var parts []string

	// Timestamp (se solicitado)
	if showTime {
		timeStr := le.Timestamp.Format("15:04:05")
		parts = append(parts, timeStyle.Render(timeStr))
	}

	// Ícone e nível
	icon := le.Level.GetIcon()
	parts = append(parts, levelStyle.Render(fmt.Sprintf("%s %s", icon, le.Level.String())))

	// Source
	if le.Source != "" {
		parts = append(parts, sourceStyle.Render(fmt.Sprintf("[%s]", le.Source)))
	}

	// Construir prefixo
	prefix := strings.Join(parts, " ")
	prefixWidth := lipgloss.Width(prefix)

	// Calcular espaço disponível para mensagem
	messageSpace := maxWidth - prefixWidth - 1
	if messageSpace < 30 {
		messageSpace = 30 // Mínimo para mensagens legíveis
	}

	var lines []string
	message := le.Message

	// Quebrar mensagem em linhas se necessário
	if len(message) <= messageSpace {
		// Mensagem cabe em uma linha
		lines = append(lines, fmt.Sprintf("%s %s", prefix, messageStyle.Render(message)))
	} else {
		// Quebrar mensagem em múltiplas linhas
		words := strings.Fields(message)
		currentLine := ""

		for i, word := range words {
			testLine := currentLine
			if testLine != "" {
				testLine += " "
			}
			testLine += word

			if len(testLine) <= messageSpace {
				currentLine = testLine
			} else {
				// Linha atual ficou muito longa, finalizar linha anterior
				if currentLine != "" {
					if len(lines) == 0 {
						// Primeira linha com prefixo
						lines = append(lines, fmt.Sprintf("%s %s", prefix, messageStyle.Render(currentLine)))
					} else {
						// Linhas subsequentes com indentação
						indent := strings.Repeat(" ", prefixWidth+1)
						lines = append(lines, fmt.Sprintf("%s%s", indent, messageStyle.Render(currentLine)))
					}
				}
				currentLine = word
			}

			// Última palavra
			if i == len(words)-1 && currentLine != "" {
				if len(lines) == 0 {
					// Primeira linha com prefixo
					lines = append(lines, fmt.Sprintf("%s %s", prefix, messageStyle.Render(currentLine)))
				} else {
					// Linhas subsequentes com indentação
					indent := strings.Repeat(" ", prefixWidth+1)
					lines = append(lines, fmt.Sprintf("%s%s", indent, messageStyle.Render(currentLine)))
				}
			}
		}
	}

	// Adicionar detalhes se houver
	if le.Details != "" {
		detailStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)

		indent := strings.Repeat(" ", prefixWidth+1)

		// Quebrar detalhes também se necessário
		details := le.Details
		detailWords := strings.Fields(details)
		currentDetailLine := ""

		for i, word := range detailWords {
			testLine := currentDetailLine
			if testLine != "" {
				testLine += " "
			}
			testLine += word

			if len(testLine) <= messageSpace-4 { // -4 para "    " prefix
				currentDetailLine = testLine
			} else {
				if currentDetailLine != "" {
					lines = append(lines, fmt.Sprintf("%s    %s", indent, detailStyle.Render(currentDetailLine)))
				}
				currentDetailLine = word
			}

			if i == len(detailWords)-1 && currentDetailLine != "" {
				lines = append(lines, fmt.Sprintf("%s    %s", indent, detailStyle.Render(currentDetailLine)))
			}
		}
	}

	return lines
}

// LogManager gerencia logs acumulativos
type LogManager struct {
	entries          []*LogEntry
	maxEntries       int
	scrollPos        int
	visibleLines     int
	showTime         bool
	showSource       bool
	minLevel         LogLevel
	manualScrollMode bool // Se está em modo scroll manual (não auto-scroll)
}

// NewLogManager cria um novo gerenciador de logs
func NewLogManager(maxEntries, visibleLines int) *LogManager {
	return &LogManager{
		entries:      make([]*LogEntry, 0),
		maxEntries:   maxEntries,
		visibleLines: visibleLines,
		showTime:     true,
		showSource:   true,
		minLevel:     LogLevelDebug,
	}
}

// Add adiciona uma nova entrada de log
func (lm *LogManager) Add(entry *LogEntry) {
	// Filtrar por nível mínimo
	if entry.Level < lm.minLevel {
		return
	}

	// Adicionar entrada
	lm.entries = append(lm.entries, entry)

	// Limitar número de entradas
	if len(lm.entries) > lm.maxEntries {
		lm.entries = lm.entries[len(lm.entries)-lm.maxEntries:]
	}

	// Auto-scroll para o final apenas se não estiver em modo manual
	if !lm.manualScrollMode {
		lm.scrollToBottom()
	}
}

// Log métodos convenientes para diferentes níveis
func (lm *LogManager) Debug(source, message string) {
	lm.Add(NewLogEntry(LogLevelDebug, source, message))
}

func (lm *LogManager) Info(source, message string) {
	lm.Add(NewLogEntry(LogLevelInfo, source, message))
}

func (lm *LogManager) Warn(source, message string) {
	lm.Add(NewLogEntry(LogLevelWarn, source, message))
}

func (lm *LogManager) Error(source, message string) {
	lm.Add(NewLogEntry(LogLevelError, source, message))
}

func (lm *LogManager) Success(source, message string) {
	lm.Add(NewLogEntry(LogLevelSuccess, source, message))
}

// Métodos com detalhes
func (lm *LogManager) ErrorWithDetails(source, message, details string) {
	entry := NewLogEntry(LogLevelError, source, message).WithDetails(details)
	lm.Add(entry)
}

func (lm *LogManager) InfoWithDetails(source, message, details string) {
	entry := NewLogEntry(LogLevelInfo, source, message).WithDetails(details)
	lm.Add(entry)
}

// Azure AD específicos
func (lm *LogManager) AzureAuth(message string) {
	lm.Info("azure-auth", message)
}

func (lm *LogManager) AzureAuthError(message, details string) {
	lm.ErrorWithDetails("azure-auth", message, details)
}

func (lm *LogManager) AzureAuthSuccess(message string) {
	lm.Success("azure-auth", message)
}

// K8s específicos
func (lm *LogManager) K8sConnection(message string) {
	lm.Info("k8s-client", message)
}

func (lm *LogManager) K8sError(message, details string) {
	lm.ErrorWithDetails("k8s-client", message, details)
}

// Session específicos
func (lm *LogManager) SessionOperation(message string) {
	lm.Info("session", message)
}

func (lm *LogManager) SessionError(message, details string) {
	lm.ErrorWithDetails("session", message, details)
}

// Scroll methods
func (lm *LogManager) ScrollUp() {
	if lm.scrollPos > 0 {
		lm.scrollPos--
	}
}

func (lm *LogManager) ScrollDown() {
	maxScroll := len(lm.entries) - lm.visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if lm.scrollPos < maxScroll {
		lm.scrollPos++
	}
}

func (lm *LogManager) scrollToBottom() {
	maxScroll := len(lm.entries) - lm.visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	lm.scrollPos = maxScroll
}

// PageUp scroll por página
func (lm *LogManager) PageUp() {
	lm.scrollPos -= lm.visibleLines
	if lm.scrollPos < 0 {
		lm.scrollPos = 0
	}
}

// PageDown scroll por página
func (lm *LogManager) PageDown() {
	maxScroll := len(lm.entries) - lm.visibleLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	lm.scrollPos += lm.visibleLines
	if lm.scrollPos > maxScroll {
		lm.scrollPos = maxScroll
	}
}

// SetManualScrollMode ativa/desativa modo de scroll manual
func (lm *LogManager) SetManualScrollMode(manual bool) {
	lm.manualScrollMode = manual
}

// IsManualScrollMode retorna se está em modo manual
func (lm *LogManager) IsManualScrollMode() bool {
	return lm.manualScrollMode
}

// EnableAutoScroll volta ao modo auto-scroll (desativa manual)
func (lm *LogManager) EnableAutoScroll() {
	lm.manualScrollMode = false
	lm.scrollToBottom() // Vai para o final imediatamente
}

// SetMinLevel define nível mínimo de log
func (lm *LogManager) SetMinLevel(level LogLevel) {
	lm.minLevel = level
}

// GetVisibleEntries retorna entradas visíveis baseado no scroll
func (lm *LogManager) GetVisibleEntries() []*LogEntry {
	if len(lm.entries) == 0 {
		return []*LogEntry{}
	}

	start := lm.scrollPos
	end := start + lm.visibleLines

	if start >= len(lm.entries) {
		start = len(lm.entries) - lm.visibleLines
		if start < 0 {
			start = 0
		}
		lm.scrollPos = start
	}

	if end > len(lm.entries) {
		end = len(lm.entries)
	}

	return lm.entries[start:end]
}

// Render renderiza logs visíveis
func (lm *LogManager) Render(maxWidth int) []string {
	visible := lm.GetVisibleEntries()
	lines := make([]string, 0, len(visible))

	for _, entry := range visible {
		rendered := entry.Render(lm.showTime, maxWidth)
		// Se a entrada tem múltiplas linhas (com detalhes), dividir
		entryLines := strings.Split(rendered, "\n")
		lines = append(lines, entryLines...)
	}

	return lines
}

// RenderFullMessages renderiza logs visíveis com mensagens completas (sem truncamento)
func (lm *LogManager) RenderFullMessages(maxWidth int) []string {
	visible := lm.GetVisibleEntries()
	lines := make([]string, 0, len(visible)*2) // Estimativa para múltiplas linhas

	for _, entry := range visible {
		entryLines := entry.RenderFullMessage(maxWidth, lm.showTime)
		lines = append(lines, entryLines...)
	}

	return lines
}

// RenderFullMessagesWithScrollIndicator renderiza logs completos com indicador de scroll
func (lm *LogManager) RenderFullMessagesWithScrollIndicator(maxWidth int) []string {
	lines := lm.RenderFullMessages(maxWidth)

	// Adicionar indicador de scroll se necessário
	if len(lm.entries) > lm.visibleLines {
		scrollIndicator := lm.getScrollIndicator()
		if len(lines) > 0 {
			// Adicionar indicador na primeira linha
			lines[0] = fmt.Sprintf("%s %s", scrollIndicator, lines[0])
		} else {
			lines = append(lines, scrollIndicator)
		}
	}

	return lines
}

// RenderWithScrollIndicator renderiza com indicador de scroll
func (lm *LogManager) RenderWithScrollIndicator(maxWidth int) []string {
	lines := lm.Render(maxWidth)

	// Adicionar indicador de scroll se necessário
	if len(lm.entries) > lm.visibleLines {
		scrollIndicator := lm.getScrollIndicator()
		if len(lines) > 0 {
			// Adicionar indicador na primeira linha
			lines[0] = fmt.Sprintf("%s %s", scrollIndicator, lines[0])
		} else {
			lines = append(lines, scrollIndicator)
		}
	}

	return lines
}

// getScrollIndicator retorna indicador de posição
func (lm *LogManager) getScrollIndicator() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Bold(true)

	start := lm.scrollPos + 1
	end := lm.scrollPos + lm.visibleLines
	if end > len(lm.entries) {
		end = len(lm.entries)
	}
	total := len(lm.entries)

	return style.Render(fmt.Sprintf("[%d-%d/%d]", start, end, total))
}

// Clear limpa todos os logs
func (lm *LogManager) Clear() {
	lm.entries = make([]*LogEntry, 0)
	lm.scrollPos = 0
}

// GetStats retorna estatísticas dos logs
func (lm *LogManager) GetStats() map[LogLevel]int {
	stats := make(map[LogLevel]int)
	for _, entry := range lm.entries {
		stats[entry.Level]++
	}
	return stats
}

// HasNewMessages verifica se há mensagens novas (últimas N entradas)
func (lm *LogManager) HasNewMessages(since time.Time) bool {
	for i := len(lm.entries) - 1; i >= 0; i-- {
		if lm.entries[i].Timestamp.After(since) {
			return true
		}
		// Se chegou em mensagem mais antiga, parar busca
		if lm.entries[i].Timestamp.Before(since) {
			break
		}
	}
	return false
}

// GetRecentErrors retorna erros recentes
func (lm *LogManager) GetRecentErrors(limit int) []*LogEntry {
	var errors []*LogEntry
	for i := len(lm.entries) - 1; i >= 0 && len(errors) < limit; i-- {
		if lm.entries[i].Level == LogLevelError {
			errors = append(errors, lm.entries[i])
		}
	}
	return errors
}