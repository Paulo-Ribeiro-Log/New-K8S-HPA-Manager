package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-hpa-manager/internal/config"
)

// EventHandler gerencia as rotas de Events
type EventHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewEventHandler cria um handler de events
func NewEventHandler(km *config.KubeConfigManager) *EventHandler {
	return &EventHandler{
		kubeManager: km,
	}
}

// EventSummary representa um resumo de Event do Kubernetes
type EventSummary struct {
	Cluster         string            `json:"cluster"`
	Namespace       string            `json:"namespace"`
	Name            string            `json:"name"`
	Type            string            `json:"type"` // Normal ou Warning
	Reason          string            `json:"reason"`
	Message         string            `json:"message"`
	Count           int32             `json:"count"`
	FirstTimestamp  string            `json:"firstTimestamp"`
	LastTimestamp   string            `json:"lastTimestamp"`
	InvolvedObject  InvolvedObjectRef `json:"involvedObject"`
	SourceComponent string            `json:"sourceComponent"`
	SourceHost      string            `json:"sourceHost,omitempty"`
	Age             string            `json:"age"`
}

// InvolvedObjectRef representa o objeto envolvido no evento
type InvolvedObjectRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid,omitempty"`
}

// List retorna events com filtros
func (h *EventHandler) List(c *gin.Context) {
	cluster := strings.TrimSpace(c.Query("cluster"))
	if cluster == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "MISSING_PARAMETER",
				"message": "Parameter 'cluster' is required",
			},
		})
		return
	}

	namespaces := parseNamespaces(c.Query("namespaces"))
	showSystem := c.Query("showSystem") == "true"
	search := strings.ToLower(c.Query("search"))
	eventType := strings.TrimSpace(c.Query("type")) // "Normal", "Warning", ou vazio (todos)

	clientset, err := h.kubeManager.GetClient(cluster)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "CLIENT_ERROR",
				"message": fmt.Sprintf("Failed to get client: %v", err),
			},
		})
		return
	}

	var events []EventSummary
	var allEvents *corev1.EventList

	if len(namespaces) == 0 {
		// Lista todos os namespaces
		allEvents, err = clientset.CoreV1().Events("").List(c.Request.Context(), metav1.ListOptions{})
	} else {
		// Lista múltiplos namespaces específicos
		allEvents = &corev1.EventList{}
		for _, ns := range namespaces {
			eventList, err := clientset.CoreV1().Events(ns).List(c.Request.Context(), metav1.ListOptions{})
			if err != nil {
				continue
			}
			allEvents.Items = append(allEvents.Items, eventList.Items...)
		}
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error": gin.H{
				"code":    "LIST_ERROR",
				"message": err.Error(),
			},
		})
		return
	}

	for _, event := range allEvents.Items {
		// Filtrar namespaces do sistema se necessário
		if !showSystem && isSystemNamespace(event.Namespace) {
			continue
		}

		// Filtrar por tipo de evento (Normal/Warning)
		if eventType != "" && event.Type != eventType {
			continue
		}

		// Filtrar por busca
		if search != "" {
			eventName := strings.ToLower(event.Name)
			namespace := strings.ToLower(event.Namespace)
			reason := strings.ToLower(event.Reason)
			message := strings.ToLower(event.Message)
			involvedObjName := strings.ToLower(event.InvolvedObject.Name)
			involvedObjKind := strings.ToLower(event.InvolvedObject.Kind)

			matchFound := strings.Contains(eventName, search) ||
				strings.Contains(namespace, search) ||
				strings.Contains(reason, search) ||
				strings.Contains(message, search) ||
				strings.Contains(involvedObjName, search) ||
				strings.Contains(involvedObjKind, search)

			if !matchFound {
				continue
			}
		}

		// Calcular idade do evento
		var age string
		if !event.LastTimestamp.IsZero() {
			age = formatAge(time.Since(event.LastTimestamp.Time))
		} else if !event.FirstTimestamp.IsZero() {
			age = formatAge(time.Since(event.FirstTimestamp.Time))
		} else {
			age = "unknown"
		}

		// Construir EventSummary
		events = append(events, EventSummary{
			Cluster:   cluster,
			Namespace: event.Namespace,
			Name:      event.Name,
			Type:      event.Type,
			Reason:    event.Reason,
			Message:   event.Message,
			Count:     event.Count,
			FirstTimestamp: func() string {
				if !event.FirstTimestamp.IsZero() {
					return event.FirstTimestamp.Format(time.RFC3339)
				}
				return ""
			}(),
			LastTimestamp: func() string {
				if !event.LastTimestamp.IsZero() {
					return event.LastTimestamp.Format(time.RFC3339)
				}
				return ""
			}(),
			InvolvedObject: InvolvedObjectRef{
				Kind:      event.InvolvedObject.Kind,
				Name:      event.InvolvedObject.Name,
				Namespace: event.InvolvedObject.Namespace,
				UID:       string(event.InvolvedObject.UID),
			},
			SourceComponent: event.Source.Component,
			SourceHost:      event.Source.Host,
			Age:             age,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"events": events,
			"count":  len(events),
		},
	})
}

// formatAge formata a duração em formato legível (ex: "2m", "5h", "3d")
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	}
}
