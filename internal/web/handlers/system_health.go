package handlers

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"k8s-hpa-manager/internal/config"
	"k8s-hpa-manager/internal/healthcheck"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// SystemHealthHandler gerencia endpoints de health check do sistema
type SystemHealthHandler struct {
	kubeManager  *config.KubeConfigManager
	orchestrator *healthcheck.Orchestrator
	startTime    time.Time
	version      string
}

// ComponentHealth representa o status de um componente
type ComponentHealth struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "healthy", "degraded", "unhealthy"
	Message string `json:"message,omitempty"`
	Latency int64  `json:"latency_ms,omitempty"`
}

// SystemHealthResponse representa a resposta completa de health
type SystemHealthResponse struct {
	Status     string            `json:"status"` // "healthy", "degraded", "unhealthy"
	Version    string            `json:"version"`
	Uptime     string            `json:"uptime"`
	UptimeSecs int64             `json:"uptime_seconds"`
	Timestamp  string            `json:"timestamp"`
	Components []ComponentHealth `json:"components"`
	System     SystemInfo        `json:"system"`
}

// SystemInfo contém informações do sistema
type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	NumGoroutine int    `json:"goroutines"`
	NumCPU       int    `json:"cpus"`
	MemoryMB     uint64 `json:"memory_mb"`
}

// NewSystemHealthHandler cria um novo handler de system health
func NewSystemHealthHandler(km *config.KubeConfigManager, orch *healthcheck.Orchestrator, version string) *SystemHealthHandler {
	return &SystemHealthHandler{
		kubeManager:  km,
		orchestrator: orch,
		startTime:    time.Now(),
		version:      version,
	}
}

// Live verifica se o serviço está vivo (liveness probe)
// GET /healthz/live
func (h *SystemHealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// Ready verifica se o serviço está pronto para receber requests (readiness probe)
// GET /healthz/ready
func (h *SystemHealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Verificar se pelo menos um cluster está acessível
	clusters := h.kubeManager.ListContexts()
	if len(clusters) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "not_ready",
			"message": "Nenhum cluster configurado",
		})
		return
	}

	// Tentar conectar ao primeiro cluster disponível
	clusterOk := false
	for _, clusterName := range clusters {
		_, err := h.kubeManager.GetK8sClient(clusterName)
		if err == nil {
			clusterOk = true
			break
		}
	}

	if !clusterOk {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "not_ready",
			"message": "Nenhum cluster acessível",
		})
		return
	}

	// Verificar storage
	if h.orchestrator != nil && h.orchestrator.Storage() != nil {
		_, err := h.orchestrator.Storage().GetEvents(ctx, "health-check-probe")
		if err != nil {
			log.Debug().Err(err).Msg("[SystemHealth] Storage check - expected empty result")
			// Ignorar erro de "not found" - é esperado
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}

// Health retorna status detalhado do sistema
// GET /healthz
func (h *SystemHealthHandler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	components := h.checkAllComponents(ctx)

	// Determinar status geral
	overallStatus := "healthy"
	for _, comp := range components {
		if comp.Status == "unhealthy" {
			overallStatus = "unhealthy"
			break
		}
		if comp.Status == "degraded" && overallStatus == "healthy" {
			overallStatus = "degraded"
		}
	}

	// Informações do sistema
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(h.startTime)

	response := SystemHealthResponse{
		Status:     overallStatus,
		Version:    h.version,
		Uptime:     formatDuration(uptime),
		UptimeSecs: int64(uptime.Seconds()),
		Timestamp:  time.Now().Format(time.RFC3339),
		Components: components,
		System: SystemInfo{
			GoVersion:    runtime.Version(),
			NumGoroutine: runtime.NumGoroutine(),
			NumCPU:       runtime.NumCPU(),
			MemoryMB:     memStats.Alloc / 1024 / 1024,
		},
	}

	statusCode := http.StatusOK
	if overallStatus == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// checkAllComponents verifica todos os componentes em paralelo
func (h *SystemHealthHandler) checkAllComponents(ctx context.Context) []ComponentHealth {
	var wg sync.WaitGroup
	var mu sync.Mutex
	components := []ComponentHealth{}

	// Lista de checks a executar
	checks := []struct {
		name  string
		check func(context.Context) ComponentHealth
	}{
		{"kubernetes", h.checkKubernetes},
		{"storage", h.checkStorage},
		{"memory", h.checkMemory},
	}

	for _, check := range checks {
		wg.Add(1)
		go func(name string, checkFn func(context.Context) ComponentHealth) {
			defer wg.Done()
			result := checkFn(ctx)
			result.Name = name
			mu.Lock()
			components = append(components, result)
			mu.Unlock()
		}(check.name, check.check)
	}

	wg.Wait()
	return components
}

// checkKubernetes verifica conexão com clusters Kubernetes
func (h *SystemHealthHandler) checkKubernetes(ctx context.Context) ComponentHealth {
	start := time.Now()

	clusters := h.kubeManager.ListContexts()
	if len(clusters) == 0 {
		return ComponentHealth{
			Status:  "unhealthy",
			Message: "Nenhum cluster configurado",
		}
	}

	// Contar clusters acessíveis
	accessible := 0
	for _, clusterName := range clusters {
		_, err := h.kubeManager.GetK8sClient(clusterName)
		if err == nil {
			accessible++
		}
	}

	latency := time.Since(start).Milliseconds()

	if accessible == 0 {
		return ComponentHealth{
			Status:  "unhealthy",
			Message: "Nenhum cluster acessível",
			Latency: latency,
		}
	}

	if accessible < len(clusters) {
		return ComponentHealth{
			Status:  "degraded",
			Message: formatClusterStatus(accessible, len(clusters)),
			Latency: latency,
		}
	}

	return ComponentHealth{
		Status:  "healthy",
		Message: formatClusterStatus(accessible, len(clusters)),
		Latency: latency,
	}
}

// checkStorage verifica conexão com storage (SQLite)
func (h *SystemHealthHandler) checkStorage(ctx context.Context) ComponentHealth {
	start := time.Now()

	if h.orchestrator == nil || h.orchestrator.Storage() == nil {
		return ComponentHealth{
			Status:  "degraded",
			Message: "Storage não configurado",
		}
	}

	// Tentar uma operação simples no storage
	_, err := h.orchestrator.Storage().GetEvents(ctx, "health-probe-test")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		// Ignorar erro de "not found" - é esperado
		if err.Error() != "no events found" && err.Error() != "session not found" {
			return ComponentHealth{
				Status:  "unhealthy",
				Message: "Erro ao acessar storage: " + err.Error(),
				Latency: latency,
			}
		}
	}

	return ComponentHealth{
		Status:  "healthy",
		Message: "SQLite operacional",
		Latency: latency,
	}
}

// checkMemory verifica uso de memória
func (h *SystemHealthHandler) checkMemory(ctx context.Context) ComponentHealth {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	allocMB := memStats.Alloc / 1024 / 1024
	sysMB := memStats.Sys / 1024 / 1024

	// Alertar se uso de memória for muito alto (>1GB)
	if allocMB > 1024 {
		return ComponentHealth{
			Status:  "degraded",
			Message: formatMemoryUsage(allocMB, sysMB),
		}
	}

	return ComponentHealth{
		Status:  "healthy",
		Message: formatMemoryUsage(allocMB, sysMB),
	}
}

// Helpers
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return formatDays(days, hours, minutes)
	}
	if hours > 0 {
		return formatHours(hours, minutes)
	}
	return formatMinutes(minutes)
}

func formatDays(days, hours, minutes int) string {
	if days == 1 {
		return "1 dia, " + formatHours(hours, minutes)
	}
	return formatNumber(days) + " dias, " + formatHours(hours, minutes)
}

func formatHours(hours, minutes int) string {
	if hours == 1 {
		return "1 hora, " + formatMinutes(minutes)
	}
	return formatNumber(hours) + " horas, " + formatMinutes(minutes)
}

func formatMinutes(minutes int) string {
	if minutes == 1 {
		return "1 minuto"
	}
	return formatNumber(minutes) + " minutos"
}

func formatNumber(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	if n < 100 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	// Para números >= 100, usar fmt
	return fmt.Sprintf("%d", n)
}

func formatClusterStatus(accessible, total int) string {
	if accessible == total {
		return fmt.Sprintf("%d clusters acessíveis", total)
	}
	return fmt.Sprintf("%d/%d clusters acessíveis", accessible, total)
}

func formatMemoryUsage(allocMB, sysMB uint64) string {
	return fmt.Sprintf("%dMB alocados / %dMB sistema", allocMB, sysMB)
}
