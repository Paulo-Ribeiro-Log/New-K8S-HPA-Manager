package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
	"k8s-hpa-manager/internal/promwal"
)

func TestPrometheusWALAnalyzeExigeClusterENamespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPrometheusWALHandler(nil, nil)
	r := gin.New()
	r.GET("/prometheus/wal/analyze", h.Analyze)

	for _, q := range []string{"", "?cluster=c1", "?namespace=monitoring"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/prometheus/wal/analyze"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: status %d, esperado 400", q, w.Code)
		}
	}
}

func walReport() promwal.Report {
	return promwal.Report{
		Name: "prom", Verdict: promwal.VerdictCleanWAL, AffectedReplicas: []string{"p-0"},
		Pods: []promwal.PodReport{{Pod: "p-0", PVC: "data-p-0"}, {Pod: "p-1", PVC: "data-p-1", Verdict: promwal.VerdictHealthy}},
		Deep: []promwal.DeepReport{{Pod: "p-0", PVC: "data-p-0"}},
	}
}

func okReq() walCleanupRequest {
	return walCleanupRequest{AnalysisID: "x", Confirm: "APAGAR", ConfirmName: "prom"}
}

func TestPlanCleanup(t *testing.T) {
	t.Run("caminho feliz usa as réplicas afetadas", func(t *testing.T) {
		got, err := planCleanup(walReport(), okReq())
		if err != nil || len(got) != 1 || got[0].PVC != "data-p-0" {
			t.Fatalf("alvos = %+v, err = %v", got, err)
		}
	})

	recusas := map[string]func(*promwal.Report, *walCleanupRequest) int{
		"sem confirmação": func(_ *promwal.Report, q *walCleanupRequest) int { q.Confirm = "apagar"; return http.StatusBadRequest },
		"nome errado": func(_ *promwal.Report, q *walCleanupRequest) int {
			q.ConfirmName = "outro"
			return http.StatusBadRequest
		},
		"veredito não é limpar_wal": func(r *promwal.Report, _ *walCleanupRequest) int {
			r.Verdict = promwal.VerdictIncreaseMemory
			return http.StatusConflict
		},
		"restauração pendente": func(r *promwal.Report, _ *walCleanupRequest) int {
			n := int32(2)
			r.PendingRestoreReplicas = &n
			return http.StatusConflict
		},
		"réplica saudável não afetada": func(_ *promwal.Report, q *walCleanupRequest) int {
			q.Replicas = []string{"p-1"}
			return http.StatusConflict
		},
		"réplica fora da análise": func(_ *promwal.Report, q *walCleanupRequest) int {
			q.Replicas = []string{"zzz"}
			return http.StatusBadRequest
		},
		"sem análise profunda": func(r *promwal.Report, _ *walCleanupRequest) int { r.Deep = nil; return http.StatusConflict },
		"sem PVC": func(r *promwal.Report, _ *walCleanupRequest) int {
			r.Pods[0].PVC = ""
			return http.StatusConflict
		},
	}
	for name, mutate := range recusas {
		t.Run(name, func(t *testing.T) {
			r, q := walReport(), okReq()
			want := mutate(&r, &q)
			if _, err := planCleanup(r, q); err == nil || err.Status != want {
				t.Fatalf("esperava recusa %d, veio %+v", want, err)
			}
		})
	}

	t.Run("force dispensa veredito e análise profunda, mas não a confirmação", func(t *testing.T) {
		r, q := walReport(), okReq()
		r.Verdict, r.Deep, q.Force, q.Replicas = promwal.VerdictInvestigate, nil, true, []string{"p-1"}
		if got, err := planCleanup(r, q); err != nil || got[0].PVC != "data-p-1" {
			t.Fatalf("force deveria liberar: %+v %v", got, err)
		}
		q.Confirm = ""
		if _, err := planCleanup(r, q); err == nil {
			t.Fatal("force não pode dispensar a confirmação")
		}
	})
}

// As rotas novas convivem com as curingas de /prometheus (gin entra em pânico no registro se conflitarem).
func TestPrometheusWALRotasNaoConflitam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPrometheusWALHandler(nil, nil)
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	r := gin.New()
	api := r.Group("/api/v1")
	api.GET("/prometheus", ok)
	api.PUT("/prometheus/:cluster/:namespace/:type/:name", ok)
	api.POST("/prometheus/:cluster/:namespace/:type/:name/rollout", ok)
	api.GET("/prometheus/wal/analyze", h.Analyze)
	api.GET("/prometheus/wal/health", h.Health)
	api.POST("/prometheus/wal/cleanup", h.Cleanup)
	api.POST("/prometheus/wal/cancel/:id", h.Cancel)
	api.POST("/prometheus/wal/restore", h.Restore)
	r.GET("/api/v1/prometheus/wal/progress/:id", h.Stream)

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/v1/prometheus/wal/analyze", http.StatusBadRequest},
		{"GET", "/api/v1/prometheus/wal/health", http.StatusBadRequest},
		{"POST", "/api/v1/prometheus/wal/cleanup", http.StatusGone}, // análise inexistente
		{"POST", "/api/v1/prometheus/wal/cancel/walclean-x", http.StatusNotFound},
		{"POST", "/api/v1/prometheus/wal/restore", http.StatusBadRequest},
		{"PUT", "/api/v1/prometheus/c1/monitoring/statefulset/prom", http.StatusNoContent},
		{"POST", "/api/v1/prometheus/c1/monitoring/statefulset/prom/rollout", http.StatusNoContent},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}")))
		if w.Code != tc.want {
			t.Errorf("%s %s = %d, esperado %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
}

func TestCleanupHistoryDates(t *testing.T) {
	ht, err := history.NewHistoryTracker(t.TempDir())
	if err != nil {
		t.Skipf("history.db indisponível: %v", err)
	}
	defer ht.Close()
	h := &PrometheusWALHandler{historyTracker: ht}
	now := time.Now()
	cleaned := map[string]interface{}{"cleaned": []interface{}{map[string]interface{}{"pvc": "data-p-0"}}}
	log := func(resource, cluster string, ago time.Duration, status string, after map[string]interface{}) {
		if err := ht.Log(context.Background(), history.HistoryEntry{
			Action: "prometheus_wal_cleanup", Resource: resource, Cluster: cluster,
			Timestamp: now.Add(-ago), Status: status, After: after,
		}); err != nil {
			t.Fatal(err)
		}
	}
	log("monitoring/prom", "c1", 24*time.Hour, "success", cleaned)                                           // conta
	log("monitoring/prom", "c1", 48*time.Hour, "failed", cleaned)                                            // falhou depois de apagar: conta
	log("monitoring/prom", "c1", 72*time.Hour, "failed", map[string]interface{}{"cleaned": []interface{}{}}) // nada apagado: não conta
	log("monitoring/prom", "c1", 40*24*time.Hour, "success", cleaned)                                        // fora da janela
	log("monitoring/prom", "c2", 24*time.Hour, "success", cleaned)                                           // outro cluster
	log("monitoring/prom-x", "c1", 24*time.Hour, "success", cleaned)                                         // outro recurso (nome parecido)

	got := h.cleanupHistoryDates("c1", "monitoring", "prom", now)
	if len(got) != 2 {
		t.Fatalf("esperava 2 limpezas, vieram %d: %v", len(got), got)
	}
	// Sem tracker (driver SQLite indisponível): não quebra.
	if got := (&PrometheusWALHandler{}).cleanupHistoryDates("c1", "monitoring", "prom", now); len(got) != 0 {
		t.Errorf("sem tracker não deveria devolver nada: %v", got)
	}
}

func TestPrometheusWALHealthUsaCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPrometheusWALHandler(nil, nil) // kubeManager nil: qualquer consulta real ao cluster entraria em pânico
	h.healthCache["c1"] = walHealthEntry{at: time.Now(), report: promwal.HealthReport{Cluster: "c1", Verifiable: true, OperatorPresent: true, Items: []promwal.HealthItem{}}}
	r := gin.New()
	r.GET("/health", h.Health)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health?cluster=c1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cluster":"c1"`) {
		t.Fatalf("deveria servir do cache: %d %s", w.Code, w.Body.String())
	}

	// Cache vencido: tentaria consultar o cluster de verdade (aqui, kubeManager nil) — não deve servir o velho.
	h.healthCache["c1"] = walHealthEntry{at: time.Now().Add(-2 * walHealthTTL), report: promwal.HealthReport{Cluster: "c1"}}
	defer func() {
		if recover() == nil {
			t.Error("cache vencido deveria ter ido ao cluster (kubeManager nil → pânico esperado no teste)")
		}
	}()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health?cluster=c1", nil))
}
