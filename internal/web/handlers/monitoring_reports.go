package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	promclient "k8s-hpa-manager/internal/monitoring/client"
	"k8s-hpa-manager/internal/monitoring/report"
	"k8s-hpa-manager/internal/storage"
)

// Relatório de métricas por aplicação (aba Monitoramento) — ver MONITORING-REPORT-PLAN.md.
// Somente leitura nos clusters, no Dynatrace e no Prometheus. A coleta roda num job em memória: o
// POST devolve o id e o GET devolve o andamento e o resultado.

const (
	reportJobTTL     = time.Hour
	reportMaxJobs    = 50
	reportJobTimeout = 20 * time.Minute
)

type reportJob struct {
	ID      string         `json:"id"`
	State   string         `json:"state"` // running | done | failed | cancelled
	Done    int            `json:"done"`
	Total   int            `json:"total"`
	Error   string         `json:"error,omitempty"`
	Result  *report.Result `json:"result,omitempty"`
	Created time.Time      `json:"created_at"`

	user   string
	cancel context.CancelFunc
}

// MonitoringReportHandler cria e acompanha os relatórios.
type MonitoringReportHandler struct {
	tokensStore *storage.UserTokensStore

	// injetáveis nos testes
	newDynatrace  func(userEmail, cluster string) (report.DynatraceAPI, error)
	newPrometheus func(cluster string) (report.RangeQuerier, error)

	mu   sync.Mutex
	jobs map[string]*reportJob
}

// NewMonitoringReportHandler cria o handler com as fontes reais.
func NewMonitoringReportHandler(tokensStore *storage.UserTokensStore) *MonitoringReportHandler {
	h := &MonitoringReportHandler{tokensStore: tokensStore, jobs: map[string]*reportJob{}}
	h.newDynatrace = func(userEmail, cluster string) (report.DynatraceAPI, error) {
		return dynatraceClientForCluster(h.tokensStore, userEmail, cluster)
	}
	h.newPrometheus = func(cluster string) (report.RangeQuerier, error) {
		return promclient.NewPrometheusClient(cluster)
	}
	return h
}

type startReportRequest struct {
	Items      []report.Item      `json:"items"`
	Start      time.Time          `json:"start"` // RFC 3339, com offset (ex.: 2026-10-01T23:00:00-03:00)
	End        time.Time          `json:"end"`
	Thresholds *report.Thresholds `json:"thresholds,omitempty"`
	// Percentiles de latência a coletar: 50, 95 e/ou 99 (vazio = só P99).
	Percentiles []int `json:"percentiles,omitempty"`
	// NodeContext liga o contexto de node (pool, uso dos nodes, throttling). Opt-in: custa consultas a mais.
	NodeContext bool `json:"node_context,omitempty"`
}

func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sweep remove jobs antigos e, se ainda passar do teto, os mais velhos já terminados. Chamar com mu.
func (h *MonitoringReportHandler) sweep() {
	now := time.Now()
	for id, j := range h.jobs {
		if j.State != "running" && now.Sub(j.Created) > reportJobTTL {
			delete(h.jobs, id)
		}
	}
	for len(h.jobs) >= reportMaxJobs {
		oldest := ""
		for id, j := range h.jobs {
			if j.State != "running" && (oldest == "" || j.Created.Before(h.jobs[oldest].Created)) {
				oldest = id
			}
		}
		if oldest == "" {
			return
		}
		delete(h.jobs, oldest)
	}
}

// Start — POST /api/v1/monitoring/reports
// Body: { "items": [{"cluster","namespace","workload"}], "start": RFC3339, "end": RFC3339, "thresholds"? }
func (h *MonitoringReportHandler) Start(c *gin.Context) {
	var req startReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido: " + err.Error()})
		return
	}
	user := c.GetString("user_email")
	pcts, perr := report.NormalizePercentiles(req.Percentiles)
	if perr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": perr.Error()})
		return
	}
	col := &report.Collector{Deps: h.deps(user, pcts)}

	// valida antes de abrir o job: erro de pedido volta na hora, com 400
	creq := report.Request{Items: req.Items, Window: report.Window{Start: req.Start, End: req.End}, Thresholds: req.Thresholds, Percentiles: pcts, NodeContext: req.NodeContext}
	if _, err := col.Prepare(creq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	h.sweep()
	if len(h.jobs) >= reportMaxJobs {
		h.mu.Unlock()
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "muitos relatórios em andamento: aguarde algum terminar"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reportJobTimeout)
	job := &reportJob{ID: newJobID(), State: "running", Total: len(req.Items), Created: time.Now(), user: user, cancel: cancel}
	h.jobs[job.ID] = job
	h.mu.Unlock()

	go func() {
		defer cancel()
		res, err := col.Run(ctx, creq, func(done, total int) {
			h.mu.Lock()
			job.Done, job.Total = done, total
			h.mu.Unlock()
		})
		h.mu.Lock()
		defer h.mu.Unlock()
		switch {
		case ctx.Err() == context.Canceled && job.State == "cancelled":
			job.Result = res
		case err != nil:
			job.State, job.Error = "failed", err.Error()
		default:
			job.State, job.Result = "done", res
		}
	}()
	c.JSON(http.StatusAccepted, gin.H{"id": job.ID, "total": job.Total})
}

// PlanWindow — GET /api/v1/monitoring/reports/plan?start=<RFC3339>&end=<RFC3339>
// Valida a janela e devolve a resolução que será usada (e os avisos), sem consultar nenhuma fonte.
// A tela mostra isso antes de coletar, sem duplicar a regra de retenção no frontend.
func (h *MonitoringReportHandler) PlanWindow(c *gin.Context) {
	start, err1 := time.Parse(time.RFC3339, c.Query("start"))
	end, err2 := time.Parse(time.RFC3339, c.Query("end"))
	if err1 != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start e end são obrigatórios, em RFC 3339 (ex.: 2026-10-01T23:00:00-03:00)"})
		return
	}
	w, warns, plan, err := (&report.Collector{}).PrepareWindow(report.Window{Start: start, End: end})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"window": w, "warnings": warns, "plan": plan, "duration_seconds": int64(w.Duration().Seconds())})
}

// Get — GET /api/v1/monitoring/reports/:id
func (h *MonitoringReportHandler) Get(c *gin.Context) {
	job, ok := h.find(c)
	if !ok {
		return
	}
	h.mu.Lock()
	snapshot := *job
	h.mu.Unlock()
	c.JSON(http.StatusOK, snapshot)
}

// Cancel — DELETE /api/v1/monitoring/reports/:id
func (h *MonitoringReportHandler) Cancel(c *gin.Context) {
	job, ok := h.find(c)
	if !ok {
		return
	}
	h.mu.Lock()
	if job.State == "running" {
		job.State = "cancelled"
		job.cancel()
	}
	h.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{"id": job.ID, "state": "cancelled"})
}

// find acha o job do usuário (o relatório de outro usuário não existe para este).
func (h *MonitoringReportHandler) find(c *gin.Context) (*reportJob, bool) {
	h.mu.Lock()
	job, ok := h.jobs[c.Param("id")]
	h.mu.Unlock()
	if !ok || job.user != c.GetString("user_email") {
		c.JSON(http.StatusNotFound, gin.H{"error": "relatório não encontrado (ou expirado)"})
		return nil, false
	}
	return job, true
}

// deps monta as fontes de dados, com um cliente por cluster dentro do relatório.
func (h *MonitoringReportHandler) deps(user string, pcts []int) report.Deps {
	var (
		mu    sync.Mutex
		dts   = map[string]dtEntry{}
		proms = map[string]promEntry{}
	)
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
	return report.Deps{
		Node: func(ctx context.Context, it report.Item, w report.Window, plan report.Plan) (*report.NodeContext, error) {
			q, err := prom(it.Cluster)
			if err != nil {
				return nil, err
			}
			return report.CollectNodeContext(ctx, q, it.Namespace, it.Workload, w, plan)
		},
		Traffic: func(ctx context.Context, it report.Item, w report.Window, plan report.Plan) (*report.Traffic, error) {
			api, err := dt(it.Cluster)
			if err != nil {
				return nil, fmt.Errorf("Dynatrace: %w", err)
			}
			return report.DynatraceTraffic(api, report.DefaultP99Floor, pcts)(ctx, it, w, plan)
		},
		Usage: func(ctx context.Context, it report.Item, w report.Window, plan report.Plan) (*report.Usage, error) {
			q, err := prom(it.Cluster)
			if err != nil {
				return nil, err
			}
			return report.CollectUsage(ctx, q, it.Namespace, it.Workload, w, plan)
		},
	}
}

type dtEntry struct {
	api report.DynatraceAPI
	err error
}

type promEntry struct {
	q   report.RangeQuerier
	err error
}
