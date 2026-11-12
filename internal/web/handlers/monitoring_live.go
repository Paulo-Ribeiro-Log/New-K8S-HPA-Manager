package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/monitoring/report"
)

// liveTimeout limita uma consulta do tempo real: a tela pede a cada 1 a 5 minutos e não acumula.
const liveTimeout = 90 * time.Second

type liveRequest struct {
	Items       []report.Item `json:"items"`
	Start       time.Time     `json:"start"` // RFC 3339; o fim é cortado em "agora"
	End         time.Time     `json:"end"`
	Percentiles []int         `json:"percentiles,omitempty"`
	MaxPoints   int           `json:"max_points,omitempty"` // pontos por série na resposta (padrão 120)
	Resources   bool          `json:"resources,omitempty"`  // réplicas, restarts, CPU e memória (Prometheus)
}

// Live — POST /api/v1/monitoring/live
// Série no tempo (RPS, 4xx/s, 5xx/s e P50/P95/P99) de cada aplicação, do Dynatrace, numa janela que
// termina agora. Somente leitura, síncrona e sem estado no servidor: a tela repete a chamada.
func (h *MonitoringReportHandler) Live(c *gin.Context) {
	var req liveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	user := c.GetString("user_email")
	var (
		mu  sync.Mutex
		dts = map[string]dtEntry{}
	)
	proms := map[string]promEntry{}
	prom := func(cluster string) (report.RangeQuerier, error) {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := proms[cluster]; ok {
			return e.q, e.err
		}
		q, err := h.newPrometheus(cluster)
		proms[cluster] = promEntry{q, err}
		return q, err
	}
	dt := func(cluster string) (report.DynatraceAPI, error) {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := dts[cluster]; ok {
			return e.api, e.err
		}
		api, err := h.newDynatrace(user, cluster)
		dts[cluster] = dtEntry{api, err}
		return api, err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), liveTimeout)
	defer cancel()
	res, err := report.CollectLive(ctx, report.LiveRequest{Items: req.Items, Window: report.Window{Start: req.Start, End: req.End}, Percentiles: req.Percentiles, MaxPoints: req.MaxPoints, Resources: req.Resources}, report.LiveDeps{Dynatrace: dt, Prometheus: prom}, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
