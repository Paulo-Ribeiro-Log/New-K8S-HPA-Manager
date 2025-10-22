package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusPanelConfig configuração do painel de status
type StatusPanelConfig struct {
	MaxWidth        int
	MaxHeight       int
	ShowProgressBars bool
	ShowLogs        bool
	MaxProgressBars int
	MaxLogLines     int
	AutoScroll      bool
	ShowTimestamps  bool
	CompactMode     bool
}

// DefaultStatusPanelConfig configuração padrão
func DefaultStatusPanelConfig() StatusPanelConfig {
	return StatusPanelConfig{
		MaxWidth:        85, // Aumentado para mensagens completas
		MaxHeight:       12, // Aumentado para mais logs visíveis
		ShowProgressBars: true,
		ShowLogs:        true,
		MaxProgressBars: 3,  // Máximo 3 progress bars visíveis
		MaxLogLines:     8,  // Mais linhas para logs
		AutoScroll:      false, // Desabilitar auto-scroll para permitir navegação manual
		ShowTimestamps:  true, // Mostrar timestamps para melhor contexto
		CompactMode:     false, // Modo expandido para mensagens completas
	}
}

// StatusPanel painel integrado de status
type StatusPanel struct {
	config          StatusPanelConfig
	progressManager *ProgressBarManager
	logManager      *LogManager
	lastUpdate      time.Time
	title           string
}

// NewStatusPanel cria um novo painel de status
func NewStatusPanel(title string, config StatusPanelConfig) *StatusPanel {
	logManager := NewLogManager(200, config.MaxLogLines) // Buffer grande, exibição limitada
	logManager.SetMinLevel(LogLevelInfo) // Filtrar DEBUG por padrão - mostrar apenas INFO, SUCCESS, WARN, ERROR

	return &StatusPanel{
		config:          config,
		progressManager: NewProgressBarManager(config.MaxProgressBars),
		logManager:      logManager,
		lastUpdate:      time.Now(),
		title:           title,
	}
}

// AddProgressBar adiciona uma nova barra de progresso
func (sp *StatusPanel) AddProgressBar(id, title string, total int) *ProgressBar {
	sp.lastUpdate = time.Now()
	return sp.progressManager.Add(id, title, total)
}

// UpdateProgress atualiza progresso
func (sp *StatusPanel) UpdateProgress(id string, current int, status string) {
	sp.lastUpdate = time.Now()
	sp.progressManager.Update(id, current, status)

	// Log da atualização se for significativa
	if current%10 == 0 || status == "completed" || status == "failed" {
		if pb, exists := sp.progressManager.bars[id]; exists {
			percentage := int(pb.GetPercentage())

			if status == "completed" {
				sp.logManager.Success("progress", fmt.Sprintf("%s: 100%% concluído", pb.Title))
			} else if status == "failed" {
				sp.logManager.Error("progress", fmt.Sprintf("%s: falhou em %d%%", pb.Title, percentage))
			}
			// Remover logs DEBUG de progresso intermediário
		}
	}
}

// CompleteProgress marca progresso como completo
func (sp *StatusPanel) CompleteProgress(id string) {
	sp.lastUpdate = time.Now()
	sp.progressManager.Complete(id)
}

// AddLog adiciona entrada de log
func (sp *StatusPanel) AddLog(level LogLevel, source, message string) {
	sp.lastUpdate = time.Now()
	sp.logManager.Add(NewLogEntry(level, source, message))
}

// Métodos convenientes para logs
func (sp *StatusPanel) Info(source, message string) {
	sp.AddLog(LogLevelInfo, source, message)
}

func (sp *StatusPanel) Success(source, message string) {
	sp.AddLog(LogLevelSuccess, source, message)
}

func (sp *StatusPanel) Warning(source, message string) {
	sp.AddLog(LogLevelWarn, source, message)
}

func (sp *StatusPanel) Error(source, message string) {
	sp.AddLog(LogLevelError, source, message)
}

func (sp *StatusPanel) Debug(source, message string) {
	sp.AddLog(LogLevelDebug, source, message)
}

// Logs específicos para Azure AD
func (sp *StatusPanel) AzureAuthStarted() {
	sp.Info("azure-auth", "🔐 Iniciando autenticação Azure AD...")
}

func (sp *StatusPanel) AzureAuthSuccess() {
	sp.Success("azure-auth", "✅ Azure AD autenticado com sucesso")
}

func (sp *StatusPanel) AzureAuthError(details string) {
	sp.Error("azure-auth", fmt.Sprintf("❌ Falha na autenticação: %s", details))
}

func (sp *StatusPanel) AzureSubscriptionSet(subscription string) {
	sp.Info("azure-config", fmt.Sprintf("📋 Subscription configurada: %s", subscription))
}

// Logs específicos para K8s
func (sp *StatusPanel) K8sConnecting(cluster string) {
	sp.Info("k8s-client", fmt.Sprintf("🔌 Conectando ao cluster: %s", cluster))
}

func (sp *StatusPanel) K8sConnected(cluster string) {
	sp.Success("k8s-client", fmt.Sprintf("✅ Conectado ao cluster: %s", cluster))
}

func (sp *StatusPanel) K8sError(operation, details string) {
	sp.Error("k8s-client", fmt.Sprintf("❌ %s: %s", operation, details))
}

// Logs específicos para Session
func (sp *StatusPanel) SessionSaved(name string) {
	sp.Success("session", fmt.Sprintf("💾 Sessão salva: %s", name))
}

func (sp *StatusPanel) SessionLoaded(name string) {
	sp.Success("session", fmt.Sprintf("📂 Sessão carregada: %s", name))
}

func (sp *StatusPanel) SessionError(operation, details string) {
	sp.Error("session", fmt.Sprintf("❌ %s: %s", operation, details))
}

// Logs específicos para HPA
func (sp *StatusPanel) HPAUpdated(cluster, namespace, name string) {
	sp.Success("hpa", fmt.Sprintf("🎯 HPA atualizado: %s/%s/%s", cluster, namespace, name))
}

func (sp *StatusPanel) HPARollout(cluster, namespace, name string, rolloutType string) {
	sp.Info("rollout", fmt.Sprintf("🔄 Iniciando rollout %s: %s/%s/%s", rolloutType, cluster, namespace, name))
}

// Logs específicos para Node Pools
func (sp *StatusPanel) NodePoolUpdated(cluster, pool string) {
	sp.Success("nodepool", fmt.Sprintf("🖥️ Node Pool atualizado: %s/%s", cluster, pool))
}

func (sp *StatusPanel) NodePoolScaling(cluster, pool string, from, to int) {
	sp.Info("nodepool", fmt.Sprintf("📊 Scaling %s/%s: %d → %d nodes", cluster, pool, from, to))
}

// Scroll methods
func (sp *StatusPanel) ScrollUp() {
	sp.logManager.ScrollUp()
}

func (sp *StatusPanel) ScrollDown() {
	sp.logManager.ScrollDown()
}

func (sp *StatusPanel) ScrollToBottom() {
	sp.logManager.scrollToBottom()
}

// PageUp scroll por página
func (sp *StatusPanel) PageUp() {
	sp.logManager.PageUp()
}

// PageDown scroll por página
func (sp *StatusPanel) PageDown() {
	sp.logManager.PageDown()
}

// SetManualScroll desabilita auto-scroll para permitir navegação manual
func (sp *StatusPanel) SetManualScroll(manual bool) {
	sp.config.AutoScroll = !manual
	sp.logManager.SetManualScrollMode(manual)
}

// EnableAutoScroll reativa auto-scroll e vai para o final
func (sp *StatusPanel) EnableAutoScroll() {
	sp.config.AutoScroll = true
	sp.logManager.EnableAutoScroll()
}

// IsManualScrollMode verifica se está em modo manual
func (sp *StatusPanel) IsManualScrollMode() bool {
	return sp.logManager.IsManualScrollMode()
}

// Clear limpa painel
func (sp *StatusPanel) Clear() {
	sp.progressManager.Clear()
	sp.logManager.Clear()
	sp.lastUpdate = time.Now()
}

// Render renderiza o painel completo
func (sp *StatusPanel) Render() string {
	var lines []string
	availableHeight := sp.config.MaxHeight

	// 1. Renderizar progress bars ativos
	if sp.config.ShowProgressBars {
		activeBars := sp.progressManager.GetActiveBars()
		recentCompleted := sp.progressManager.GetRecentCompleted(1) // Apenas 1 completado recente

		progressLines := 0
		for _, pb := range activeBars {
			if progressLines >= sp.config.MaxProgressBars {
				break
			}
			if sp.config.CompactMode {
				lines = append(lines, pb.RenderCompact())
			} else {
				lines = append(lines, pb.Render())
			}
			progressLines++
		}

		// Adicionar uma barra completada recente se houver espaço
		if progressLines < sp.config.MaxProgressBars && len(recentCompleted) > 0 {
			pb := recentCompleted[0]
			if time.Since(pb.StartTime) < time.Minute*2 { // Só mostrar se completou há menos de 2 min
				if sp.config.CompactMode {
					lines = append(lines, pb.RenderCompact())
				} else {
					lines = append(lines, pb.Render())
				}
				progressLines++
			}
		}

		availableHeight -= progressLines
	}

	// 2. Adicionar separador se há progress bars e logs
	if len(lines) > 0 && sp.config.ShowLogs && availableHeight > 0 {
		separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))
		lines = append(lines, separatorStyle.Render(strings.Repeat("─", sp.config.MaxWidth-4)))
		availableHeight--
	}

	// 3. Renderizar logs
	if sp.config.ShowLogs && availableHeight > 0 {
		// Ajustar linhas visíveis do log manager
		originalVisibleLines := sp.logManager.visibleLines
		sp.logManager.visibleLines = availableHeight

		// Se auto-scroll, manter na parte inferior
		if sp.config.AutoScroll {
			sp.logManager.scrollToBottom()
		}

		logLines := sp.logManager.RenderFullMessagesWithScrollIndicator(sp.config.MaxWidth - 2)

		// Limitar ao espaço disponível
		if len(logLines) > availableHeight {
			logLines = logLines[len(logLines)-availableHeight:]
		}

		lines = append(lines, logLines...)

		// Restaurar configuração original
		sp.logManager.visibleLines = originalVisibleLines
	}

	// 4. Preencher linhas vazias se necessário
	for len(lines) < sp.config.MaxHeight {
		lines = append(lines, "")
	}

	// 5. Truncar se exceder altura máxima
	if len(lines) > sp.config.MaxHeight {
		lines = lines[:sp.config.MaxHeight]
	}

	return strings.Join(lines, "\n")
}

// RenderCompact renderiza versão ainda mais compacta
func (sp *StatusPanel) RenderCompact() string {
	var lines []string

	// Progress bars (máximo 2 linhas)
	activeBars := sp.progressManager.GetActiveBars()
	for i, pb := range activeBars {
		if i >= 2 {
			break
		}
		lines = append(lines, pb.RenderCompact())
	}

	// Logs (preencher resto do espaço)
	remainingLines := sp.config.MaxHeight - len(lines)
	if remainingLines > 0 {
		sp.logManager.visibleLines = remainingLines
		logLines := sp.logManager.Render(sp.config.MaxWidth - 2)

		if len(logLines) > remainingLines {
			logLines = logLines[len(logLines)-remainingLines:]
		}

		lines = append(lines, logLines...)
	}

	// Preencher espaço restante
	for len(lines) < sp.config.MaxHeight {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// GetStats retorna estatísticas do painel
func (sp *StatusPanel) GetStats() map[string]interface{} {
	activeProgress, completedProgress, failedProgress := sp.progressManager.GetStats()
	logStats := sp.logManager.GetStats()

	return map[string]interface{}{
		"active_progress":    activeProgress,
		"completed_progress": completedProgress,
		"failed_progress":    failedProgress,
		"log_stats":          logStats,
		"last_update":        sp.lastUpdate,
		"total_entries":      len(sp.logManager.entries),
	}
}

// HasActivity verifica se há atividade recente
func (sp *StatusPanel) HasActivity() bool {
	return time.Since(sp.lastUpdate) < time.Minute*5
}

// SetConfig atualiza configuração
func (sp *StatusPanel) SetConfig(config StatusPanelConfig) {
	sp.config = config
	sp.logManager.visibleLines = config.MaxLogLines
}