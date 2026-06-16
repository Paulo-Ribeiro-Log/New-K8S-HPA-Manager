package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"k8s-hpa-manager/internal/config"
)

// AutoDiscoverProgress representa o progresso da auto-descoberta
type AutoDiscoverProgress struct {
	Phase       string `json:"phase"`              // discovering, processing, saving, completed, error
	Message     string `json:"message"`            // Mensagem de progresso
	Current     int    `json:"current"`            // Cluster atual sendo processado
	Total       int    `json:"total"`              // Total de clusters
	ClusterName string `json:"clusterName"`        // Nome do cluster atual
	Success     int    `json:"success"`            // Clusters configurados com sucesso
	Errors      int    `json:"errors"`             // Clusters com erro
	Provider    string `json:"provider,omitempty"` // "aks" | "eks" — para o frontend diferenciar
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

	// Executar auto-descoberta AKS + EKS em paralelo
	go func() {
		defer close(progressChan)

		manager := h.kubeManager
		if manager == nil {
			progressChan <- AutoDiscoverProgress{Phase: "error", Message: "❌ Erro: KubeConfigManager não inicializado"}
			return
		}

		progressChan <- AutoDiscoverProgress{
			Phase:   "discovering",
			Message: "🔍 Iniciando auto-descoberta AKS + EKS + GKE em paralelo...",
		}

		// Resultado acumulado de todos os providers
		type providerResult struct {
			aksConfigs []config.ClusterConfig
			eksConfigs []config.EKSClusterConfig
			gkeConfigs []config.GKEClusterConfig
			aksErrors  []error
			eksErrors  []error
			gkeErrors  []error
		}
		pr := &providerResult{}
		var mu sync.Mutex
		var wg sync.WaitGroup

		// Goroutine AKS
		wg.Add(1)
		go func() {
			defer wg.Done()
			aksConfigs, aksErrors := manager.AutoDiscoverAllClusters(func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "processing", Message: msg, Provider: "aks"}
			})
			mu.Lock()
			pr.aksConfigs = aksConfigs
			pr.aksErrors = aksErrors
			mu.Unlock()
		}()

		// Goroutine EKS
		wg.Add(1)
		go func() {
			defer wg.Done()
			eksConfigs, eksErrors := manager.AutoDiscoverEKSClusters(func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "processing", Message: msg, Provider: "eks"}
			})
			mu.Lock()
			pr.eksConfigs = eksConfigs
			pr.eksErrors = eksErrors
			mu.Unlock()
		}()

		// Goroutine GKE
		wg.Add(1)
		go func() {
			defer wg.Done()
			gkeConfigs, gkeErrors := manager.AutoDiscoverGKEClusters(func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "processing", Message: msg, Provider: "gke"}
			})
			mu.Lock()
			pr.gkeConfigs = gkeConfigs
			pr.gkeErrors = gkeErrors
			mu.Unlock()
		}()

		wg.Wait()

		// Salvar AKS
		aksSuccess := len(pr.aksConfigs)
		if aksSuccess > 0 {
			progressChan <- AutoDiscoverProgress{
				Phase: "saving", Message: fmt.Sprintf("[AKS] 💾 Salvando %d configurações...", aksSuccess),
				Provider: "aks", Success: aksSuccess,
			}
			if err := manager.SaveClusterConfigs(pr.aksConfigs, func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "saving", Message: msg, Provider: "aks"}
			}); err != nil {
				progressChan <- AutoDiscoverProgress{Phase: "error", Message: fmt.Sprintf("[AKS] ❌ Erro ao salvar: %v", err), Provider: "aks"}
			}
		}

		// Salvar EKS
		eksSuccess := len(pr.eksConfigs)
		if eksSuccess > 0 {
			progressChan <- AutoDiscoverProgress{
				Phase: "saving", Message: fmt.Sprintf("[EKS] 💾 Salvando %d configurações...", eksSuccess),
				Provider: "eks", Success: eksSuccess,
			}
			if err := manager.SaveEKSClusterConfigs(pr.eksConfigs, func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "saving", Message: msg, Provider: "eks"}
			}); err != nil {
				progressChan <- AutoDiscoverProgress{Phase: "error", Message: fmt.Sprintf("[EKS] ❌ Erro ao salvar: %v", err), Provider: "eks"}
			}
		}

		// Salvar GKE
		gkeSuccess := len(pr.gkeConfigs)
		if gkeSuccess > 0 {
			progressChan <- AutoDiscoverProgress{
				Phase: "saving", Message: fmt.Sprintf("[GKE] 💾 Salvando %d configurações...", gkeSuccess),
				Provider: "gke", Success: gkeSuccess,
			}
			if err := manager.SaveGKEClusterConfigs(pr.gkeConfigs, func(msg string) {
				progressChan <- AutoDiscoverProgress{Phase: "saving", Message: msg, Provider: "gke"}
			}); err != nil {
				progressChan <- AutoDiscoverProgress{Phase: "error", Message: fmt.Sprintf("[GKE] ❌ Erro ao salvar: %v", err), Provider: "gke"}
			}
		}

		totalErrors := len(pr.aksErrors) + len(pr.eksErrors) + len(pr.gkeErrors)
		progressChan <- AutoDiscoverProgress{
			Phase:   "completed",
			Message: fmt.Sprintf("✅ Auto-descoberta concluída! AKS: %d | EKS: %d | GKE: %d | erros: %d", aksSuccess, eksSuccess, gkeSuccess, totalErrors),
			Success: aksSuccess + eksSuccess + gkeSuccess,
			Errors:  totalErrors,
		}

		allErrs := append(append(pr.aksErrors, pr.eksErrors...), pr.gkeErrors...)
		for _, err := range allErrs {
			progressChan <- AutoDiscoverProgress{Phase: "error", Message: fmt.Sprintf("⚠️  %v", err)}
		}
	}()

	// Aguardar conclusão
	<-done
}

// HandleAutoDiscoverSync executa auto-descoberta AKS + EKS + GKE de forma síncrona (sem SSE).
func (h *AutoDiscoverHandler) HandleAutoDiscoverSync(c *gin.Context) {
	manager := h.kubeManager
	if manager == nil {
		c.JSON(500, gin.H{"error": "KubeConfigManager not initialized"})
		return
	}

	var (
		aksConfigs []config.ClusterConfig
		eksConfigs []config.EKSClusterConfig
		gkeConfigs []config.GKEClusterConfig
		aksErrors  []error
		eksErrors  []error
		gkeErrors  []error
		wg         sync.WaitGroup
		mu         sync.Mutex
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		cfgs, errs := manager.AutoDiscoverAllClusters(nil)
		mu.Lock()
		aksConfigs, aksErrors = cfgs, errs
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		cfgs, errs := manager.AutoDiscoverEKSClusters(nil)
		mu.Lock()
		eksConfigs, eksErrors = cfgs, errs
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		cfgs, errs := manager.AutoDiscoverGKEClusters(nil)
		mu.Lock()
		gkeConfigs, gkeErrors = cfgs, errs
		mu.Unlock()
	}()
	wg.Wait()

	if len(aksConfigs) > 0 {
		if err := manager.SaveClusterConfigs(aksConfigs, nil); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to save AKS configs: %v", err)})
			return
		}
	}
	if len(eksConfigs) > 0 {
		if err := manager.SaveEKSClusterConfigs(eksConfigs, nil); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to save EKS configs: %v", err)})
			return
		}
	}
	if len(gkeConfigs) > 0 {
		if err := manager.SaveGKEClusterConfigs(gkeConfigs, nil); err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("Failed to save GKE configs: %v", err)})
			return
		}
	}

	allErrors := append(append(aksErrors, eksErrors...), gkeErrors...)
	c.JSON(200, gin.H{
		"aks_success": len(aksConfigs),
		"eks_success": len(eksConfigs),
		"gke_success": len(gkeConfigs),
		"errors":      len(allErrors),
		"error_messages": func() []string {
			msgs := make([]string, len(allErrors))
			for i, e := range allErrors {
				msgs[i] = e.Error()
			}
			return msgs
		}(),
	})
}
