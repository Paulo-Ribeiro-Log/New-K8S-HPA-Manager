package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
)

// AutoDiscoverProgress representa o progresso da auto-descoberta
type AutoDiscoverProgress struct {
	Phase       string `json:"phase"`       // discovering, processing, saving, completed, error
	Message     string `json:"message"`     // Mensagem de progresso
	Current     int    `json:"current"`     // Cluster atual sendo processado
	Total       int    `json:"total"`       // Total de clusters
	ClusterName string `json:"clusterName"` // Nome do cluster atual
	Success     int    `json:"success"`     // Clusters configurados com sucesso
	Errors      int    `json:"errors"`      // Clusters com erro
}

// AutoDiscoverHandler gerencia auto-descoberta de clusters
type AutoDiscoverHandler struct {
	kubeManager *config.KubeConfigManager
}

// NewAutoDiscoverHandler cria um novo handler de auto-descoberta
func NewAutoDiscoverHandler(km *config.KubeConfigManager) *AutoDiscoverHandler {
	return &AutoDiscoverHandler{
		kubeManager: km,
	}
}

// HandleAutoDiscover executa auto-descoberta de clusters com SSE progress
func (h *AutoDiscoverHandler) HandleAutoDiscover(c *gin.Context) {
	// Configurar SSE
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		c.JSON(500, gin.H{"error": "Streaming unsupported"})
		return
	}

	// Canal para mensagens de progresso
	progressChan := make(chan AutoDiscoverProgress, 100)
	done := make(chan struct{})

	// Contexto com timeout de 10 minutos
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()

	// Goroutine para enviar eventos SSE
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case progress, ok := <-progressChan:
				if !ok {
					return
				}
				// Enviar evento SSE manualmente
				data, _ := json.Marshal(progress)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}()

	// Executar auto-descoberta
	go func() {
		defer close(progressChan)

		// Fase 1: Descobrindo clusters
		progressChan <- AutoDiscoverProgress{
			Phase:   "discovering",
			Message: "🔍 Descobrindo clusters do kubeconfig...",
			Current: 0,
			Total:   0,
		}

		// Obter KubeConfigManager
		manager := h.kubeManager
		if manager == nil {
			progressChan <- AutoDiscoverProgress{
				Phase:   "error",
				Message: "❌ Erro: KubeConfigManager não inicializado",
			}
			return
		}

		// Descobrir clusters disponíveis
		clusters := manager.DiscoverClusters()
		totalClusters := len(clusters)

		if totalClusters == 0 {
			progressChan <- AutoDiscoverProgress{
				Phase:   "completed",
				Message: "⚠️  Nenhum cluster encontrado no kubeconfig",
				Total:   0,
			}
			return
		}

		progressChan <- AutoDiscoverProgress{
			Phase:   "processing",
			Message: fmt.Sprintf("📊 Encontrados %d clusters. Iniciando auto-descoberta paralela...", totalClusters),
			Total:   totalClusters,
		}

		// Fase 2: Processando clusters (com callback de progresso)
		successCount := 0
		errorCount := 0
		currentCluster := 0

		configs, errors := manager.AutoDiscoverAllClusters(func(msg string) {
			currentCluster++
			progressChan <- AutoDiscoverProgress{
				Phase:   "processing",
				Message: msg,
				Current: currentCluster,
				Total:   totalClusters,
			}
		})

		// Contabilizar sucesso e erros
		successCount = len(configs)
		errorCount = len(errors)

		// Fase 3: Salvando configurações
		if len(configs) > 0 {
			progressChan <- AutoDiscoverProgress{
				Phase:   "saving",
				Message: fmt.Sprintf("💾 Salvando %d configurações...", len(configs)),
				Current: totalClusters,
				Total:   totalClusters,
				Success: successCount,
				Errors:  errorCount,
			}

			if err := manager.SaveClusterConfigs(configs, func(msg string) {
				progressChan <- AutoDiscoverProgress{
					Phase:   "saving",
					Message: msg,
					Current: totalClusters,
					Total:   totalClusters,
					Success: successCount,
					Errors:  errorCount,
				}
			}); err != nil {
				progressChan <- AutoDiscoverProgress{
					Phase:   "error",
					Message: fmt.Sprintf("❌ Erro ao salvar configurações: %v", err),
					Current: totalClusters,
					Total:   totalClusters,
					Success: successCount,
					Errors:  errorCount,
				}
				return
			}
		}

		// Fase 4: Completed
		var finalMessage string
		if errorCount > 0 {
			finalMessage = fmt.Sprintf("✅ Auto-descoberta concluída! %d clusters configurados, %d com erros", successCount, errorCount)
		} else {
			finalMessage = fmt.Sprintf("✅ Auto-descoberta concluída com sucesso! %d clusters configurados", successCount)
		}

		progressChan <- AutoDiscoverProgress{
			Phase:   "completed",
			Message: finalMessage,
			Current: totalClusters,
			Total:   totalClusters,
			Success: successCount,
			Errors:  errorCount,
		}

		// Enviar erros se houver
		if len(errors) > 0 {
			for _, err := range errors {
				progressChan <- AutoDiscoverProgress{
					Phase:   "error",
					Message: fmt.Sprintf("⚠️  %v", err),
					Current: totalClusters,
					Total:   totalClusters,
					Success: successCount,
					Errors:  errorCount,
				}
			}
		}
	}()

	// Aguardar conclusão
	<-done
}

// HandleAutoDiscoverSync executa auto-descoberta de forma síncrona (sem SSE)
// Útil para chamadas API simples que não precisam de feedback em tempo real
func (h *AutoDiscoverHandler) HandleAutoDiscoverSync(c *gin.Context) {
	manager := h.kubeManager
	if manager == nil {
		c.JSON(500, gin.H{"error": "KubeConfigManager not initialized"})
		return
	}

	// Executar auto-descoberta
	configs, errors := manager.AutoDiscoverAllClusters(nil)

	// Salvar configurações se houver
	if len(configs) > 0 {
		if err := manager.SaveClusterConfigs(configs, nil); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to save configs: %v", err)})
			return
		}
	}

	// Responder com resultado
	c.JSON(200, gin.H{
		"success":       len(configs),
		"errors":        len(errors),
		"totalClusters": len(manager.DiscoverClusters()),
		"errorMessages": errors,
	})
}
