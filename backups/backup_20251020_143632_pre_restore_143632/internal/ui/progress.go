package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ProgressBarStyle define o estilo visual do progress bar
type ProgressBarStyle struct {
	FilledChar   string
	EmptyChar    string
	Width        int
	ShowPercent  bool
	ShowTime     bool
	BracketStyle lipgloss.Style
	FilledStyle  lipgloss.Style
	EmptyStyle   lipgloss.Style
	TextStyle    lipgloss.Style
}

// DefaultProgressBarStyle retorna o estilo padrão Rich Python
func DefaultProgressBarStyle() ProgressBarStyle {
	return ProgressBarStyle{
		FilledChar:  "━",
		EmptyChar:   "╌",
		Width:       50,
		ShowPercent: true,
		ShowTime:    true,
		BracketStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")),
		FilledStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")), // Verde
		EmptyStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")), // Cinza escuro
		TextStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F3F4F6")), // Branco suave
	}
}

// CompactProgressBarStyle retorna estilo compacto para painéis pequenos
func CompactProgressBarStyle() ProgressBarStyle {
	return ProgressBarStyle{
		FilledChar:  "█",
		EmptyChar:   "░",
		Width:       30,
		ShowPercent: true,
		ShowTime:    false,
		BracketStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")),
		FilledStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6")), // Azul
		EmptyStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#1F2937")),
		TextStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB")),
	}
}

// ProgressBar representa uma barra de progresso
type ProgressBar struct {
	ID          string
	Title       string
	Current     int
	Total       int
	StartTime   time.Time
	Style       ProgressBarStyle
	Status      string // running, completed, failed, paused
	Error       string
	Metadata    map[string]interface{}
}

// NewProgressBar cria uma nova barra de progresso
func NewProgressBar(id, title string, total int) *ProgressBar {
	return &ProgressBar{
		ID:        id,
		Title:     title,
		Current:   0,
		Total:     total,
		StartTime: time.Now(),
		Style:     DefaultProgressBarStyle(),
		Status:    "running",
		Metadata:  make(map[string]interface{}),
	}
}

// Update atualiza o progresso
func (pb *ProgressBar) Update(current int, status string) {
	pb.Current = current
	if status != "" {
		pb.Status = status
	}
}

// SetError define um erro
func (pb *ProgressBar) SetError(err string) {
	pb.Error = err
	pb.Status = "failed"
}

// Complete marca como completo
func (pb *ProgressBar) Complete() {
	pb.Current = pb.Total
	pb.Status = "completed"
}

// GetPercentage retorna a porcentagem de progresso
func (pb *ProgressBar) GetPercentage() float64 {
	if pb.Total == 0 {
		return 0
	}
	return float64(pb.Current) / float64(pb.Total) * 100
}

// GetDuration retorna a duração desde o início
func (pb *ProgressBar) GetDuration() time.Duration {
	return time.Since(pb.StartTime)
}

// GetETA estima o tempo restante
func (pb *ProgressBar) GetETA() time.Duration {
	if pb.Current == 0 {
		return 0
	}

	elapsed := pb.GetDuration()
	rate := float64(pb.Current) / elapsed.Seconds()
	remaining := pb.Total - pb.Current

	if rate > 0 {
		return time.Duration(float64(remaining)/rate) * time.Second
	}
	return 0
}

// getProgressColor retorna a cor baseada na porcentagem (vermelho -> amarelo -> verde)
// Sistema de cores dinâmicas:
//   0-24%:  🔴 Vermelho (#EF4444) - Inicial/crítico
//  25-49%:  🟠 Laranja  (#F59E0B) - Progredindo
//  50-74%:  🟡 Amarelo  (#EAB308) - Meio caminho
//  75-99%:  🟢 Verde claro (#84CC16) - Quase lá
//   100%:   ✅ Verde completo (#10B981) - Concluído
func (pb *ProgressBar) getProgressColor() lipgloss.Color {
	percentage := pb.GetPercentage()

	if percentage < 25 {
		return lipgloss.Color("#EF4444") // 🔴 Vermelho
	} else if percentage < 50 {
		return lipgloss.Color("#F59E0B") // 🟠 Laranja
	} else if percentage < 75 {
		return lipgloss.Color("#EAB308") // 🟡 Amarelo
	} else if percentage < 100 {
		return lipgloss.Color("#84CC16") // 🟢 Verde claro
	} else {
		return lipgloss.Color("#10B981") // ✅ Verde completo
	}
}

// Render renderiza a barra de progresso
func (pb *ProgressBar) Render() string {
	percentage := pb.GetPercentage()
	filledWidth := int(float64(pb.Style.Width) * percentage / 100)
	emptyWidth := pb.Style.Width - filledWidth

	// Construir a barra
	filled := strings.Repeat(pb.Style.FilledChar, filledWidth)
	empty := strings.Repeat(pb.Style.EmptyChar, emptyWidth)

	// Aplicar estilos com cor dinâmica baseada no progresso
	progressColor := pb.getProgressColor()
	dynamicFilledStyle := lipgloss.NewStyle().Foreground(progressColor)
	styledFilled := dynamicFilledStyle.Render(filled)
	styledEmpty := pb.Style.EmptyStyle.Render(empty)

	// Construir texto de status
	var statusText strings.Builder

	// Porcentagem
	if pb.Style.ShowPercent {
		statusText.WriteString(fmt.Sprintf(" %.0f%%", percentage))
	}

	// Tempo
	if pb.Style.ShowTime {
		duration := pb.GetDuration()
		if duration > time.Minute {
			statusText.WriteString(fmt.Sprintf(" (%dm%ds)", int(duration.Minutes()), int(duration.Seconds())%60))
		} else {
			statusText.WriteString(fmt.Sprintf(" (%.0fs)", duration.Seconds()))
		}
	}

	// Status específico
	var statusIndicator string
	switch pb.Status {
	case "completed":
		statusIndicator = " ✅"
	case "failed":
		statusIndicator = " ❌"
	case "paused":
		statusIndicator = " ⏸️"
	case "running":
		statusIndicator = " 🔄"
	}

	// Título e mensagem final
	var finalText string
	if pb.Error != "" {
		finalText = pb.Error
	} else {
		finalText = pb.Title
	}

	// Montar linha completa
	return fmt.Sprintf("%s%s%s%s %s%s",
		styledFilled,
		styledEmpty,
		pb.Style.TextStyle.Render(statusText.String()),
		statusIndicator,
		pb.Style.TextStyle.Render(finalText),
		"",
	)
}

// RenderCompact renderiza versão compacta com cor dinâmica
func (pb *ProgressBar) RenderCompact() string {
	percentage := pb.GetPercentage()

	var statusIcon string
	switch pb.Status {
	case "completed":
		statusIcon = "✅"
	case "failed":
		statusIcon = "❌"
	case "running":
		statusIcon = "🔄"
	default:
		statusIcon = "⏳"
	}

	// Aplicar cor dinâmica ao ícone e porcentagem quando em execução
	progressColor := pb.getProgressColor()
	coloredStatusIcon := lipgloss.NewStyle().Foreground(progressColor).Render(statusIcon)
	coloredPercentage := lipgloss.NewStyle().Foreground(progressColor).Render(fmt.Sprintf("%.0f%%", percentage))

	return fmt.Sprintf("%s %s %s",
		coloredStatusIcon,
		coloredPercentage,
		pb.Title,
	)
}

// ProgressBarManager gerencia múltiplas barras de progresso
type ProgressBarManager struct {
	bars      map[string]*ProgressBar
	completed []string // IDs de barras completadas para histórico
	maxActive int      // Máximo de barras ativas visíveis
}

// NewProgressBarManager cria um novo gerenciador
func NewProgressBarManager(maxActive int) *ProgressBarManager {
	return &ProgressBarManager{
		bars:      make(map[string]*ProgressBar),
		completed: make([]string, 0),
		maxActive: maxActive,
	}
}

// Add adiciona uma nova barra de progresso
func (pbm *ProgressBarManager) Add(id, title string, total int) *ProgressBar {
	pb := NewProgressBar(id, title, total)
	pbm.bars[id] = pb
	return pb
}

// Update atualiza uma barra existente
func (pbm *ProgressBarManager) Update(id string, current int, status string) {
	if pb, exists := pbm.bars[id]; exists {
		pb.Update(current, status)

		// Se completou, mover para histórico
		if status == "completed" || status == "failed" {
			pbm.moveToCompleted(id)
		}
	}
}

// Complete marca uma barra como completa
func (pbm *ProgressBarManager) Complete(id string) {
	if pb, exists := pbm.bars[id]; exists {
		pb.Complete()
		pbm.moveToCompleted(id)
	}
}

// SetError define erro em uma barra
func (pbm *ProgressBarManager) SetError(id, error string) {
	if pb, exists := pbm.bars[id]; exists {
		pb.SetError(error)
		pbm.moveToCompleted(id)
	}
}

// Remove remove uma barra
func (pbm *ProgressBarManager) Remove(id string) {
	delete(pbm.bars, id)
}

// moveToCompleted move uma barra para o histórico
func (pbm *ProgressBarManager) moveToCompleted(id string) {
	pbm.completed = append(pbm.completed, id)

	// Limitar histórico para evitar acúmulo excessivo
	if len(pbm.completed) > pbm.maxActive*2 {
		pbm.completed = pbm.completed[len(pbm.completed)-pbm.maxActive*2:]
	}
}

// GetActiveBars retorna barras ativas
func (pbm *ProgressBarManager) GetActiveBars() []*ProgressBar {
	var active []*ProgressBar
	for _, pb := range pbm.bars {
		if pb.Status == "running" || pb.Status == "paused" {
			active = append(active, pb)
		}
	}
	return active
}

// GetRecentCompleted retorna barras recém completadas
func (pbm *ProgressBarManager) GetRecentCompleted(limit int) []*ProgressBar {
	var recent []*ProgressBar

	// Pegar últimas completadas
	start := len(pbm.completed) - limit
	if start < 0 {
		start = 0
	}

	for i := start; i < len(pbm.completed); i++ {
		id := pbm.completed[i]
		if pb, exists := pbm.bars[id]; exists {
			recent = append(recent, pb)
		}
	}

	return recent
}

// RenderAll renderiza todas as barras visíveis
func (pbm *ProgressBarManager) RenderAll() []string {
	var lines []string

	// Barras ativas primeiro
	active := pbm.GetActiveBars()
	for _, pb := range active {
		lines = append(lines, pb.Render())
	}

	// Barras recém completadas
	recent := pbm.GetRecentCompleted(3)
	for _, pb := range recent {
		lines = append(lines, pb.Render())
	}

	return lines
}

// Clear limpa todas as barras
func (pbm *ProgressBarManager) Clear() {
	pbm.bars = make(map[string]*ProgressBar)
	pbm.completed = make([]string, 0)
}

// GetStats retorna estatísticas
func (pbm *ProgressBarManager) GetStats() (active, completed, failed int) {
	for _, pb := range pbm.bars {
		switch pb.Status {
		case "running", "paused":
			active++
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}
	return
}