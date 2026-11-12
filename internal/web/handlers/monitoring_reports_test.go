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
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/dynatrace"
	promclient "k8s-hpa-manager/internal/monitoring/client"
	"k8s-hpa-manager/internal/monitoring/report"
)

type fakeDTAPI struct {
	noPG bool
	log  *selLog // registra as consultas de série (opcional)
}

// selLog guarda os metricSelector pedidos ao fake do Dynatrace (seguro para uso concorrente).
type selLog struct {
	mu   sync.Mutex
	list []string
}

func (l *selLog) add(sel string) {
	l.mu.Lock()
	l.list = append(l.list, sel)
	l.mu.Unlock()
}

func (l *selLog) has(part string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.list {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func (f fakeDTAPI) ResolveEntryServices(_ context.Context, cluster, ns, wl string) ([]dynatrace.ServiceRef, []string, error) {
	if f.noPG {
		return nil, nil, dynatrace.ErrNoProcessGroup
	}
	return []dynatrace.ServiceRef{{ID: "S1", Name: "FreteController", Type: "WEB_SERVICE"}}, nil, nil
}

func (f fakeDTAPI) QueryServiceSeries(_ context.Context, sel string, _ []string, _, _ time.Time, _ string, _ float64) (map[string]dynatrace.NullableSeries, error) {
	if f.log != nil {
		f.log.add(sel)
	}
	// o cliente real já devolve os valores na escala final (percentis em ms)
	v := 600.0 // requisições por intervalo
	switch {
	case strings.Contains(sel, "percentile(50)"):
		v = 120
	case strings.Contains(sel, "percentile(95)"):
		v = 900
	case strings.Contains(sel, "response.time"): // percentile(99)
		v = 1500
	case strings.Contains(sel, "fourxx"):
		v = 10
	case strings.Contains(sel, "fivexx"):
		v = 0
	}
	return map[string]dynatrace.NullableSeries{"S1": {Times: []int64{0, 60000}, Values: []float64{v, math.NaN()}}}, nil
}

type fakePromQ struct{}

func (fakePromQ) QueryRange(_ context.Context, q string, _, _ time.Time, _ time.Duration) (*promclient.QueryRangeResult, error) {
	r := &promclient.QueryRangeResult{Status: "success"}
	add := func(metric map[string]string, vals ...string) {
		var v [][]interface{}
		for i, x := range vals {
			v = append(v, []interface{}{float64(i), x})
		}
		r.Data.Result = append(r.Data.Result, struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		}{Metric: metric, Values: v})
	}
	switch {
	case strings.Contains(q, "count by (node) (kube_pod_info"):
		add(map[string]string{"node": "aks-p-vmss000001"}, "2")
		add(map[string]string{"node": "aks-p-vmss00000f"}, "1")
	case strings.HasPrefix(q, "kube_node_labels"):
		add(map[string]string{"node": "aks-p-vmss000001", "label_agentpool": "calculofret2", "label_node_kubernetes_io_instance_type": "Standard_D4s_v5"}, "1")
		add(map[string]string{"node": "aks-p-vmss00000f", "label_agentpool": "calculofret2"}, "1")
	case strings.Contains(q, "node_cpu_seconds_total"):
		add(map[string]string{"node": "aks-p-vmss000001"}, "20", "76")
		add(map[string]string{"node": "aks-p-vmss00000f"}, "10", "30")
	case strings.Contains(q, "node_memory_MemAvailable_bytes"):
		add(map[string]string{"node": "aks-p-vmss000001"}, "40")
	case strings.Contains(q, "cfs_throttled"):
		add(map[string]string{}, "0.1", "0.4")
	default: // consumo dos pods (CPU/memória em % do request e do limit)
		add(map[string]string{}, "10", "20")
	}
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
	g.GET("/plan", h.PlanWindow)
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
		"percentiles": []int{50, 95, 99},
		"items":       []map[string]string{{"cluster": "c1", "namespace": "calculo-de-fretes-prd", "workload": "frete-hub"}},
		"start":       now.Add(-6 * time.Hour).Format(time.RFC3339),
		"end":         now.Add(-time.Hour).Format(time.RFC3339),
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
	if row.Traffic == nil || row.Traffic.P99PeakMs == nil || *row.Traffic.P99PeakMs != 1500 || row.Traffic.P50PeakMs == nil || *row.Traffic.P50PeakMs != 120 ||
		row.Traffic.P95PeakMs == nil || *row.Traffic.P95PeakMs != 900 || row.Flags.P99 != "red" ||
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

func TestRelatorioPlanoDaJanela(t *testing.T) {
	r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
	now := time.Now().UTC()
	get := func(start, end time.Time) *httptest.ResponseRecorder {
		return chamar(r, http.MethodGet, "/reports/plan?start="+start.Format(time.RFC3339)+"&end="+end.Format(time.RFC3339), "ana@x", nil)
	}
	// janela recente: 1 min, sem aviso de pico aproximado
	w := get(now.Add(-6*time.Hour), now.Add(-time.Hour))
	var out struct {
		Plan            report.Plan `json:"plan"`
		DurationSeconds int64       `json:"duration_seconds"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Plan.DTResolution != "1m" || out.Plan.PeakApprox || out.DurationSeconds != 5*3600 {
		t.Errorf("janela recente: %d %s", w.Code, w.Body.String())
	}
	// janela de meses atrás: resolução de 1 h e pico aproximado
	w = get(now.Add(-120*24*time.Hour), now.Add(-120*24*time.Hour+3*time.Hour))
	out = struct {
		Plan            report.Plan `json:"plan"`
		DurationSeconds int64       `json:"duration_seconds"`
	}{}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Plan.DTResolution != "1h" || !out.Plan.PeakApprox {
		t.Errorf("janela antiga: %d %s", w.Code, w.Body.String())
	}
	// inválidas: 400
	if w := get(now.Add(-time.Hour), now.Add(-2*time.Hour)); w.Code != http.StatusBadRequest {
		t.Errorf("fim antes do início: %d", w.Code)
	}
	if w := get(now.Add(-500*24*time.Hour), now.Add(-499*24*time.Hour)); w.Code != http.StatusBadRequest {
		t.Errorf("além da retenção: %d", w.Code)
	}
	if w := chamar(r, http.MethodGet, "/reports/plan?start=ontem", "ana@x", nil); w.Code != http.StatusBadRequest {
		t.Errorf("datas inválidas: %d", w.Code)
	}
}

func TestRelatorioPercentisEscolhidos(t *testing.T) {
	roda := func(t *testing.T, pcts any) (reportJob, *selLog, *httptest.ResponseRecorder) {
		t.Helper()
		log := &selLog{}
		r, _ := montaRelatorios(t, fakeDTAPI{log: log}, nil)
		body := pedido()
		if pcts == nil {
			delete(body, "percentiles")
		} else {
			body["percentiles"] = pcts
		}
		w := chamar(r, http.MethodPost, "/reports", "ana@x", body)
		if w.Code != http.StatusAccepted {
			return reportJob{}, log, w
		}
		var created struct{ ID string }
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		return esperar(t, r, created.ID, "ana@x"), log, w
	}

	// sem escolha: só P99 (o comportamento de antes); P50 e P95 nem são consultados
	j, log, _ := roda(t, nil)
	tr := j.Result.Rows[0].Traffic
	if tr == nil || tr.P99PeakMs == nil || tr.P50PeakMs != nil || tr.P95PeakMs != nil {
		t.Fatalf("padrão deveria ser só P99: %+v", tr)
	}
	if log.has("percentile(50)") || log.has("percentile(95)") || !log.has("percentile(99)") {
		t.Errorf("consultas inesperadas: %v", log.list)
	}
	if len(j.Result.Percentiles) != 1 || j.Result.Percentiles[0] != 99 {
		t.Errorf("o resultado deve dizer quais percentis foram coletados: %v", j.Result.Percentiles)
	}

	// P50 e P95 (sem P99): P99 não é consultado, não aparece e não gera selo
	j, log, _ = roda(t, []int{95, 50})
	tr = j.Result.Rows[0].Traffic
	if tr.P50PeakMs == nil || *tr.P50PeakMs != 120 || tr.P95PeakMs == nil || *tr.P95PeakMs != 900 || tr.P99PeakMs != nil {
		t.Errorf("P50/P95 escolhidos: %+v", tr)
	}
	if log.has("percentile(99)") {
		t.Errorf("P99 não foi escolhido e não deve ser consultado: %v", log.list)
	}
	if j.Result.Rows[0].Flags.P99 != "" {
		t.Errorf("sem P99 não há selo de P99: %+v", j.Result.Rows[0].Flags)
	}
	if got := j.Result.Percentiles; len(got) != 2 || got[0] != 50 || got[1] != 95 {
		t.Errorf("percentis do resultado (ordenados): %v", got)
	}

	// percentil que não existe: 400, sem abrir job
	if _, _, w := roda(t, []int{90}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "percentil 90") {
		t.Errorf("percentil inválido: %d %s", w.Code, w.Body.String())
	}
}

func TestRelatorioContextoDeNode(t *testing.T) {
	roda := func(t *testing.T, node any) reportJob {
		t.Helper()
		r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
		body := pedido()
		if node != nil {
			body["node_context"] = node
		}
		w := chamar(r, http.MethodPost, "/reports", "ana@x", body)
		if w.Code != http.StatusAccepted {
			t.Fatalf("POST: %d %s", w.Code, w.Body.String())
		}
		var created struct{ ID string }
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		return esperar(t, r, created.ID, "ana@x")
	}

	// sem pedir (padrão): não há contexto de node e o resultado diz que não foi coletado
	j := roda(t, nil)
	if j.Result.NodeContext || j.Result.Rows[0].Node != nil {
		t.Errorf("por padrão não coleta node: %+v", j.Result.Rows[0].Node)
	}

	// pedindo: pool, nodes, uso e throttling
	j = roda(t, true)
	n := j.Result.Rows[0].Node
	if !j.Result.NodeContext || n == nil {
		t.Fatalf("esperava o contexto de node: %+v", j.Result)
	}
	if n.Nodes != 2 || len(n.Pools) != 1 || n.Pools[0].Name != "calculofret2" || n.Pools[0].Nodes != 2 {
		t.Errorf("nodes/pool: %+v", n)
	}
	if n.CPU == nil || *n.CPU.WorstPct != 76 || n.Mem == nil || n.Mem.NodesWithData != 1 || n.Throttle == nil || n.Throttle.Max != 0.4 {
		t.Errorf("uso dos nodes e throttling: cpu=%+v mem=%+v throttle=%+v", n.CPU, n.Mem, n.Throttle)
	}
	if len(j.Result.Rows[0].Errors) != 0 {
		t.Errorf("erros inesperados: %v", j.Result.Rows[0].Errors)
	}
}

func TestRelatorioPlanoAvisaRetencaoDoPrometheus(t *testing.T) {
	r, _ := montaRelatorios(t, fakeDTAPI{}, nil)
	now := time.Now().UTC()
	var out struct {
		Warnings []string `json:"warnings"`
	}
	w := chamar(r, http.MethodGet, "/reports/plan?start="+now.Add(-30*24*time.Hour).Format(time.RFC3339)+"&end="+now.Add(-29*24*time.Hour).Format(time.RFC3339), "ana@x", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "retenção do Prometheus") {
		t.Errorf("o plano deve avisar antes de coletar: %d %s", w.Code, w.Body.String())
	}
	out.Warnings = nil
	w = chamar(r, http.MethodGet, "/reports/plan?start="+now.Add(-6*time.Hour).Format(time.RFC3339)+"&end="+now.Add(-time.Hour).Format(time.RFC3339), "ana@x", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Warnings) != 0 {
		t.Errorf("janela recente não tem aviso: %d %s", w.Code, w.Body.String())
	}
}

func TestTempoRealDevolveSeriesSemEstado(t *testing.T) {
	r, h := montaRelatorios(t, fakeDTAPI{}, nil)
	r.POST("/live", h.Live)
	now := time.Now().UTC()
	body := map[string]any{
		"percentiles": []int{99},
		"items":       []map[string]string{{"cluster": "c1", "namespace": "ns", "workload": "frete-hub"}},
		"start":       now.Add(-time.Hour).Format(time.RFC3339),
		"end":         now.Format(time.RFC3339),
	}
	w := chamar(r, http.MethodPost, "/live", "u@x", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res report.LiveResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Series) != 1 || res.Series[0].Workload != "frete-hub" || res.Resolution != "1m" {
		t.Errorf("resposta: %+v", res)
	}
	// NaN do Dynatrace não pode quebrar o JSON (ficaria 500): chegou até aqui, então virou nulo

	body["items"] = []map[string]string{}
	if w := chamar(r, http.MethodPost, "/live", "u@x", body); w.Code != http.StatusBadRequest {
		t.Errorf("sem itens: status %d, esperado 400", w.Code)
	}
}

func TestTempoRealComRecursosDoPrometheus(t *testing.T) {
	r, h := montaRelatorios(t, fakeDTAPI{}, nil)
	r.POST("/live", h.Live)
	now := time.Now().UTC()
	body := map[string]any{
		"percentiles": []int{50, 99},
		"resources":   true,
		"items":       []map[string]string{{"cluster": "c1", "namespace": "ns", "workload": "frete-hub"}},
		"start":       now.Add(-time.Hour).Format(time.RFC3339),
		"end":         now.Format(time.RFC3339),
	}
	w := chamar(r, http.MethodPost, "/live", "u@x", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res report.LiveResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	// o fake de Prometheus responde "consumo" a qualquer consulta; o que importa aqui é o contrato:
	// o campo existe quando pedido e some quando não é pedido
	if res.Series[0].Resources == nil {
		t.Error("com resources=true a série deve trazer o bloco do Prometheus")
	}
	delete(body, "resources")
	w = chamar(r, http.MethodPost, "/live", "u@x", body)
	var sem report.LiveResult
	_ = json.Unmarshal(w.Body.Bytes(), &sem)
	if sem.Series[0].Resources != nil {
		t.Error("sem resources o Prometheus não deve ser consultado")
	}
}
