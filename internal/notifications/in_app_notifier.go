package notifications

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InAppNotifier armazena notificações em memória para exibição no frontend
// Solução KISS para ambientes corporativos onde instalações Windows são restritas
type InAppNotifier struct {
	notifications []InAppNotification
	mutex         sync.RWMutex
	maxBuffer     int
}

// InAppNotification representa uma notificação in-app
type InAppNotification struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
	Read      bool      `json:"read"`
	// Metadados para navegação
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	HPAName   string `json:"hpaName,omitempty"`
}

// NewInAppNotifier cria um novo notificador in-app
func NewInAppNotifier() *InAppNotifier {
	return &InAppNotifier{
		notifications: make([]InAppNotification, 0),
		maxBuffer:     50, // Manter últimas 50 notificações
	}
}

// AddNotification adiciona uma nova notificação ao buffer
func (n *InAppNotifier) AddNotification(title, message, severity, cluster, namespace, hpaName string) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	notification := InAppNotification{
		ID:        uuid.New().String(),
		Title:     title,
		Message:   message,
		Severity:  severity,
		Timestamp: time.Now(),
		Read:      false,
		Cluster:   cluster,
		Namespace: namespace,
		HPAName:   hpaName,
	}

	// Adicionar no início (mais recente primeiro)
	n.notifications = append([]InAppNotification{notification}, n.notifications...)

	// Limitar tamanho do buffer
	if len(n.notifications) > n.maxBuffer {
		n.notifications = n.notifications[:n.maxBuffer]
	}

	fmt.Printf("📬 Notificação adicionada: %s - %s\n", severity, title)
	return nil
}

// GetUnreadNotifications retorna todas as notificações não lidas
func (n *InAppNotifier) GetUnreadNotifications() []InAppNotification {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	unread := make([]InAppNotification, 0)
	for _, notif := range n.notifications {
		if !notif.Read {
			unread = append(unread, notif)
		}
	}

	return unread
}

// GetAllNotifications retorna todas as notificações
func (n *InAppNotifier) GetAllNotifications() []InAppNotification {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	// Retornar cópia para evitar race conditions
	notifications := make([]InAppNotification, len(n.notifications))
	copy(notifications, n.notifications)

	return notifications
}

// MarkAsRead marca uma notificação como lida
func (n *InAppNotifier) MarkAsRead(id string) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for i := range n.notifications {
		if n.notifications[i].ID == id {
			n.notifications[i].Read = true
			return nil
		}
	}

	return fmt.Errorf("notificação não encontrada: %s", id)
}

// MarkAllAsRead marca todas as notificações como lidas
func (n *InAppNotifier) MarkAllAsRead() error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	for i := range n.notifications {
		n.notifications[i].Read = true
	}

	return nil
}

// ClearAll limpa todas as notificações
func (n *InAppNotifier) ClearAll() error {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	n.notifications = make([]InAppNotification, 0)
	return nil
}

// GetUnreadCount retorna o número de notificações não lidas
func (n *InAppNotifier) GetUnreadCount() int {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	count := 0
	for _, notif := range n.notifications {
		if !notif.Read {
			count++
		}
	}

	return count
}
