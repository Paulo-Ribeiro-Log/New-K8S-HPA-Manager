package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/displayenv"
)

// SystemDisplayHandler expõe o teste de ambiente gráfico (WSLg) e de navegador do Windows.
// A detecção consulta o registro do Windows (reg.exe) no WSL, então o resultado fica em cache por
// alguns segundos; ?refresh=true refaz.
type SystemDisplayHandler struct {
	mu      sync.Mutex
	cached  displayenv.Report
	expires time.Time
	detect  func(ctx context.Context) displayenv.Report
}

const systemDisplayTTL = 30 * time.Second

func NewSystemDisplayHandler() *SystemDisplayHandler {
	return &SystemDisplayHandler{detect: func(ctx context.Context) displayenv.Report {
		return displayenv.Detect(ctx, displayenv.DefaultProbe())
	}}
}

// GetDisplay — GET /api/v1/system/display[?refresh=true]. Só leitura.
func (h *SystemDisplayHandler) GetDisplay(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Query("refresh") == "true" || time.Now().After(h.expires) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		h.cached, h.expires = h.detect(ctx), time.Now().Add(systemDisplayTTL)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.cached})
}
