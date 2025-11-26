package notifications

import (
	"fmt"
	"sync"
	"time"
)

// NotificationManager gerencia todas as notificações da aplicação
type NotificationManager struct {
	windowsNotifier *WindowsNotifier
	inAppNotifier   *InAppNotifier
	enabled         bool

	// Deduplicação de alertas (evitar spam)
	alertCache map[string]time.Time
	cacheMutex sync.RWMutex

	// Configurações
	cooldownPeriod time.Duration // Tempo mínimo entre notificações do mesmo alerta
}

// NewNotificationManager cria um novo gerenciador de notificações
func NewNotificationManager() *NotificationManager {
	return &NotificationManager{
		windowsNotifier: NewWindowsNotifier(),
		inAppNotifier:   NewInAppNotifier(),
		enabled:         true,
		alertCache:      make(map[string]time.Time),
		cooldownPeriod:  5 * time.Minute, // 5 minutos entre alertas idênticos
	}
}

// EnableNotifications habilita/desabilita notificações
func (m *NotificationManager) EnableNotifications(enabled bool) {
	m.enabled = enabled
}

// IsEnabled verifica se notificações estão habilitadas
func (m *NotificationManager) IsEnabled() bool {
	return m.enabled && m.windowsNotifier.IsEnabled()
}

// NotifyAlert envia notificação de alerta com deduplicação
func (m *NotificationManager) NotifyAlert(alertName, severity, cluster, namespace, hpaName, message string) error {
	if !m.enabled {
		return nil // Notificações desabilitadas
	}

	// Criar chave única para o alerta
	alertKey := fmt.Sprintf("%s:%s:%s:%s:%s", cluster, namespace, hpaName, alertName, severity)

	// Verificar cooldown (evitar spam)
	m.cacheMutex.RLock()
	lastSent, exists := m.alertCache[alertKey]
	m.cacheMutex.RUnlock()

	if exists && time.Since(lastSent) < m.cooldownPeriod {
		// Alerta recente, não enviar novamente
		return nil
	}

	// Enviar notificação Windows (tentativa - pode falhar em ambientes restritos)
	_ = m.windowsNotifier.SendAlertNotification(alertName, severity, cluster, namespace, hpaName, message)

	// SEMPRE adicionar à fila in-app (funciona sempre, sem restrições corporativas)
	title := fmt.Sprintf("%s: %s", getEmojiForSeverity(severity), alertName)
	fullMessage := fmt.Sprintf("Cluster: %s\nNamespace: %s\nHPA: %s\n\n%s",
		cluster, namespace, hpaName, message)

	if err := m.inAppNotifier.AddNotification(title, fullMessage, severity); err != nil {
		return fmt.Errorf("falha ao adicionar notificação in-app: %w", err)
	}

	// Atualizar cache
	m.cacheMutex.Lock()
	m.alertCache[alertKey] = time.Now()
	m.cacheMutex.Unlock()

	// Limpar alertas antigos do cache (older than 1 hour)
	go m.cleanupAlertCache()

	return nil
}

// getEmojiForSeverity retorna o emoji adequado para a severidade
func getEmojiForSeverity(severity string) string {
	switch severity {
	case "critical":
		return "🔴 CRÍTICO"
	case "warning":
		return "🟡 AVISO"
	default:
		return "ℹ️ INFO"
	}
}

// NotifyCordonDrain envia notificação de operação Cordon/Drain
func (m *NotificationManager) NotifyCordonDrain(phase, nodeName, message string) error {
	if !m.enabled {
		return nil
	}

	return m.windowsNotifier.SendCordonDrainNotification(phase, nodeName, message)
}

// NotifyApplySuccess envia notificação de aplicação bem-sucedida
func (m *NotificationManager) NotifyApplySuccess(resourceType string, count int) error {
	if !m.enabled {
		return nil
	}

	return m.windowsNotifier.SendApplySuccessNotification(resourceType, count)
}

// NotifyCustom envia notificação customizada
func (m *NotificationManager) NotifyCustom(title, message, severity string) error {
	if !m.enabled {
		return nil
	}

	return m.windowsNotifier.SendNotification(NotificationOptions{
		Title:    title,
		Message:  message,
		Severity: severity,
		Duration: "Short",
	})
}

// TestNotification envia uma notificação de teste
func (m *NotificationManager) TestNotification() error {
	return m.windowsNotifier.TestNotification()
}

// cleanupAlertCache remove alertas antigos do cache
func (m *NotificationManager) cleanupAlertCache() {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	for key, timestamp := range m.alertCache {
		if timestamp.Before(cutoff) {
			delete(m.alertCache, key)
		}
	}
}

// SetCooldownPeriod configura o período de cooldown entre alertas
func (m *NotificationManager) SetCooldownPeriod(duration time.Duration) {
	m.cooldownPeriod = duration
}

// GetAlertCacheSize retorna o número de alertas no cache
func (m *NotificationManager) GetAlertCacheSize() int {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	return len(m.alertCache)
}

// GetInAppNotifier retorna o notificador in-app
func (m *NotificationManager) GetInAppNotifier() *InAppNotifier {
	return m.inAppNotifier
}
