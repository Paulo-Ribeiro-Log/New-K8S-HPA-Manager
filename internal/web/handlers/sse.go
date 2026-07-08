package handlers

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/web/sse"
)

// ProgressStreamHandler - Gerenciador global do ProgressTracker
var progressTracker *sse.ProgressTracker

func init() {
	progressTracker = sse.NewProgressTracker()
}

// GetProgressTracker - Retorna instância global do ProgressTracker
func GetProgressTracker() *sse.ProgressTracker {
	return progressTracker
}

// HandleProgressStream - Handler SSE para streaming de progresso
// GET /api/v1/nodepools/progress/:operationId
func HandleProgressStream(c *gin.Context) {
	operationID := c.Param("operationId")
	if operationID == "" {
		c.JSON(400, gin.H{"error": "Operation ID is required"})
		return
	}

	// Headers SSE obrigatórios
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Criar novo cliente SSE
	client := sse.NewClient(operationID)
	progressTracker.AddClient(client)

	// Enviar evento inicial de conexão
	initialEvent := sse.ProgressEvent{
		ID:        operationID,
		Type:      "connected",
		Phase:     "started",
		Message:   "Conexão SSE estabelecida",
		Progress:  0.0,
		Timestamp: time.Now(),
	}
	c.Writer.WriteString(sse.FormatSSE(initialEvent))
	c.Writer.Flush()

	// Context cancelation para cleanup
	ctx := c.Request.Context()

	// Loop de envio de eventos
	for {
		select {
		case event, ok := <-client.Channel:
			if !ok {
				// Canal fechado - operação concluída
				return
			}

			// Enviar evento SSE
			message := sse.FormatSSE(event)
			if _, err := c.Writer.WriteString(message); err != nil {
				// Client desconectou
				progressTracker.RemoveClient(operationID)
				return
			}
			c.Writer.Flush()

			// Se evento é de conclusão ou erro, fechar conexão
			if event.Type == "complete" || event.Type == "error" {
				// Aguardar 2s para garantir que mensagem foi entregue
				time.Sleep(2 * time.Second)
				progressTracker.RemoveClient(operationID)
				return
			}

		case <-ctx.Done():
			// Client desconectou (browser fechado ou timeout)
			progressTracker.RemoveClient(operationID)
			return
		}
	}
}

// HandleProgressStatus - Retorna status atual de uma operação
// GET /api/v1/nodepools/progress/:operationId/status
func HandleProgressStatus(c *gin.Context) {
	operationID := c.Param("operationId")
	if operationID == "" {
		c.JSON(400, gin.H{"error": "Operation ID is required"})
		return
	}

	client, exists := progressTracker.GetClient(operationID)
	if !exists {
		c.JSON(404, gin.H{
			"error":   "Operation not found",
			"message": fmt.Sprintf("No active operation with ID: %s", operationID),
		})
		return
	}

	c.JSON(200, gin.H{
		"operation_id": operationID,
		"connected":    !client.IsClosed,
		"channel_size": len(client.Channel),
	})
}
