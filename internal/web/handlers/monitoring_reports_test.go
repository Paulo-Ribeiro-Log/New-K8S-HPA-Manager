package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/dynatrace"
	promclient "k8s-hpa-manager/internal/monitoring/client"
	"k8s-hpa-manager/internal/monitoring/report"
)

type fakeDTAPI struct{ noPG bool }

func (f fakeDTAPI) ResolveEntryServices(_ context.Context, cluster, ns, wl string) ([]dynatrace.ServiceRef, []string, error) {
	if f.noPG {
		return nil, nil, dynatrace.ErrNoProcessGroup
	}
	return []dynatrace.ServiceRef{{ID: "S1", Name: "FreteController", Type: "WEB_SERVICE"}}, nil, nil
}

func (f fakeDTAPI) QueryServiceSeries(_ context.Context, sel string, _ []string, _, _ time.Time, _ string, _ float64) (map[string]dynatrace.NullableSeries, error) {
	// o cliente real já devolve os valores na escala final (P99 em ms)
	v := 600.0 // requisições por intervalo
	switch {
	case strings.Contains(sel, "response.time"):
		v = 1500
	case strings.Contains(sel, "fourxx"):
		v = 10
	case strings.Contains(sel, "fivexx"):
		v = 0
	}
	return map[string]dynatrace.NullableSeries{"S1": {Times: []int64{0, 60000}, Values: []float64{v, math.NaN()}}}, nil
}

type fakePromQ struct{}

func (fakePromQ) QueryRange(_ context.Context, _ string, _, _ time.Time, _ time.Duration) (*promclient.QueryRangeResult, error) {
	r := &promclient.QueryRangeResult{Status: "success"}
	r.Data.Result = append(r.Data.Result, struct {
		Metric map[string]string `json:"metric"`
		Values [][]interface{}   `json:"values"`
	}{Values: [][]interface{}{{0.0, "10"}, {60.0, "20"}}})
	return r, nil
}

func montaRelatorios(t *testing.T, dt report.DynatraceAPI, dtErr error) (*gin.Engine, *MonitoringReportHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewMonitoringReportHandler(nil)
	h.newDynatrace = func(_, _ string) (report.DynatraceAPI, error) { return dt, dtErr }
	h.newPrometheus = func(string) (report.RangeQuerier, error) { return fakePromQ{}, nil }
	r := gin.New()
	g := r.Group("/reports", func(c *gin.Context) { c.Set("user_email", c.GetHeader("X-User")) })
	g.POST("", h.Start)
	g.GET("/:id", h.Get)
	g.DELETE("/:id", h.Cancel)
	return r, h
}

func chamar(r *gin.Engine, method, path, user string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-User", user)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func pedido() map[string]any {
	now := time.Now().UTC()
	return map[string]any{
		"items": []map[string]string{{"cluster": "c1", "namespace": "calculo-de-fretes-prd", "workload": "frete-hub"}},
		"start": now.Add(-6 * time.Hour).Format(time.RFC3339),
		"end":   now.Add(-time.Hour).Format(time.RFC3339),
	}
}

func esperar(t *testing.T, r *gin.Engine, id, user string) reportJob {
	t.Helper()
	for i := 0; i < 200; i++ {
		w := chamar(r, http.MethodGet, "/reports/"+id, user, nil)
		var j reportJob
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatalf("resposta: %s", w.Body.String())
		}
		if j.State != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("o relatório não terminou")
	return reportJob{}
}

func TestRelatorioDeMetricas(t *testing.T) {
	r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
	w := chamar(r, http.MethodPost, "/reports", "ana@x", pedido())
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	j := esperar(t, r, created.ID, "ana@x")
	if j.State != "done" || j.Result == nil || len(j.Result.Rows) != 1 || j.Done != 1 {
		t.Fatalf("job: %+v", j)
	}
	row := j.Result.Rows[0]
	if row.Traffic == nil || row.Traffic.P99PeakMs == nil || *row.Traffic.P99PeakMs != 1500 || row.Flags.P99 != "red" ||
		row.Flags.Err4 != "warn" || row.Flags.Err5 != "" || *row.Traffic.Err4xx != 10 || *row.Traffic.Requests != 600 {
		t.Errorf("tráfego/selo: %+v flags=%+v", row.Traffic, row.Flags)
	}
	if row.Usage == nil || row.Usage.CPUReq == nil || row.Usage.CPUReq.Max != 20 || len(row.Errors) != 0 {
		t.Errorf("consumo: %+v erros=%v", row.Usage, row.Errors)
	}
	if j.Result.Plan.DTResolution != "1m" || j.Result.Plan.P99Resolution != "5m" {
		t.Errorf("plano: %+v", j.Result.Plan)
	}
}

func TestRelatorioFalhaDeUmaFonteNaoDerrubaALinha(t *testing.T) {
	r, _ := montaRelatorios(t, nil, errors.New("Dynatrace não configurado"))
	w := chamar(r, http.MethodPost, "/reports", "ana@x", pedido())
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	j := esperar(t, r, created.ID, "ana@x")
	row := j.Result.Rows[0]
	if j.State != "done" || row.Traffic != nil || row.Usage == nil || len(row.Errors) != 1 {
		t.Errorf("sem Dynatrace o consumo deve aparecer: %+v", row)
	}
}

func TestRelatorioPedidoInvalido(t *testing.T) {
	r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
	now := time.Now().UTC()
	casos := map[string]map[string]any{
		"sem itens":        {"items": []any{}, "start": now.Add(-2 * time.Hour), "end": now.Add(-time.Hour)},
		"fim antes":        {"items": pedido()["items"], "start": now.Add(-time.Hour), "end": now.Add(-2 * time.Hour)},
		"além da retenção": {"items": pedido()["items"], "start": now.Add(-500 * 24 * time.Hour), "end": now.Add(-499 * 24 * time.Hour)},
	}
	for nome, body := range casos {
		if w := chamar(r, http.MethodPost, "/reports", "ana@x", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d %s", nome, w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewBufferString("{nao é json"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("JSON inválido: %d", w.Code)
	}
}

func TestRelatorioIsoladoPorUsuarioECancelavel(t *testing.T) {
	r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
	w := chamar(r, http.MethodPost, "/reports", "ana@x", pedido())
	var created struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	esperar(t, r, created.ID, "ana@x")

	if w := chamar(r, http.MethodGet, "/reports/"+created.ID, "outro@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("relatório de outro usuário: %d", w.Code)
	}
	if w := chamar(r, http.MethodGet, "/reports/inexistente", "ana@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("id inexistente: %d", w.Code)
	}
	if w := chamar(r, http.MethodDelete, "/reports/"+created.ID, "outro@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("cancelar relatório alheio: %d", w.Code)
	}
}

func TestRelatorioSweepRemoveVelhosETravaOTeto(t *testing.T) {
	_, h := montaRelatorios(t, fakeDTAPI{}, nil)
	h.mu.Lock()
	h.jobs["velho"] = &reportJob{ID: "velho", State: "done", Created: time.Now().Add(-2 * reportJobTTL)}
	h.jobs["novo"] = &reportJob{ID: "novo", State: "done", Created: time.Now()}
	h.sweep()
	_, velho := h.jobs["velho"]
	_, novo := h.jobs["novo"]
	h.mu.Unlock()
	if velho || !novo {
		t.Errorf("sweep: velho=%v novo=%v", velho, novo)
	}
}
