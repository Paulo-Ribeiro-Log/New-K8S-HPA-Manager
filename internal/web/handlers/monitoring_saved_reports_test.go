package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/monitoring/report"
	"k8s-hpa-manager/internal/storage"
)

func savedRouter(t *testing.T, withStore bool) (*gin.Engine, *MonitoringReportHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reports := NewMonitoringReportHandler(nil)
	var store *storage.SavedReportsStore
	if withStore {
		var err error
		store, err = storage.NewSavedReportsStore(filepath.Join(t.TempDir(), "saved.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
	}
	h := NewMonitoringSavedReportsHandler(store, reports)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_email", c.GetHeader("X-User")) })
	r.POST("/reports/:id/save", h.Save)
	r.GET("/saved", h.List)
	r.GET("/saved/:id", h.Get)
	r.DELETE("/saved/:id", h.Delete)
	return r, reports
}

func addJob(h *MonitoringReportHandler, id, user, state string, rows ...report.Row) {
	res := &report.Result{
		Window: report.Window{Start: time.Date(2026, 11, 11, 17, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 11, 19, 0, 0, 0, time.UTC)},
		Rows:   rows, Percentiles: []int{99}, GeneratedAt: time.Date(2026, 11, 11, 19, 1, 0, 0, time.UTC),
	}
	job := &reportJob{ID: id, State: state, user: user}
	if state == "done" {
		job.Result = res
	}
	h.mu.Lock()
	h.jobs[id] = job
	h.mu.Unlock()
}

func rowOf(cluster, workload string) report.Row {
	return report.Row{Item: report.Item{Cluster: cluster, Namespace: "ns", Workload: workload}}
}

func TestSalvarListarAbrirEApagarORelatorio(t *testing.T) {
	r, reports := savedRouter(t, true)
	addJob(reports, "j1", "a@x", "done", rowOf("aks-a", "w1"), rowOf("aks-a", "w2"), rowOf("aks-b", "w3"))

	w := chamar(r, http.MethodPost, "/reports/j1/save", "a@x", map[string]string{"name": "  Stress test 11/11 "})
	if w.Code != http.StatusOK {
		t.Fatalf("salvar = %d %s", w.Code, w.Body)
	}
	var meta storage.SavedReportMeta
	_ = json.Unmarshal(w.Body.Bytes(), &meta)
	if meta.ID == 0 || meta.Name != "Stress test 11/11" || meta.Apps != 3 || meta.Clusters != 2 || meta.SizeBytes == 0 {
		t.Fatalf("meta: %+v", meta)
	}

	w = chamar(r, http.MethodGet, "/saved", "a@x", nil)
	var list struct {
		Reports []storage.SavedReportMeta `json:"reports"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list.Reports) != 1 || list.Reports[0].ID != meta.ID {
		t.Fatalf("lista = %d %s", w.Code, w.Body)
	}
	if bytesHave(w.Body.Bytes(), `"rows"`) {
		t.Error("a lista não deve trazer o resultado (só os metadados)")
	}

	w = chamar(r, http.MethodGet, fmt.Sprintf("/saved/%d", meta.ID), "a@x", nil)
	var got struct {
		Meta   storage.SavedReportMeta `json:"meta"`
		Result report.Result           `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
		t.Fatalf("abrir = %d %v %s", w.Code, err, w.Body)
	}
	if len(got.Result.Rows) != 3 || got.Result.Rows[2].Cluster != "aks-b" || len(got.Result.Percentiles) != 1 || got.Result.Percentiles[0] != 99 || !got.Result.GeneratedAt.Equal(time.Date(2026, 11, 11, 19, 1, 0, 0, time.UTC)) || got.Meta.Name != "Stress test 11/11" {
		t.Errorf("o relatório aberto não é o que foi salvo: %+v", got.Result)
	}

	if w := chamar(r, http.MethodDelete, fmt.Sprintf("/saved/%d", meta.ID), "a@x", nil); w.Code != 200 {
		t.Fatalf("apagar = %d %s", w.Code, w.Body)
	}
	if w := chamar(r, http.MethodGet, fmt.Sprintf("/saved/%d", meta.ID), "a@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("depois de apagar deve dar 404, deu %d", w.Code)
	}
}

func bytesHave(b []byte, s string) bool { return strings.Contains(string(b), s) }

func TestSoSalvaRelatorioConcluidoEDoProprioUsuario(t *testing.T) {
	r, reports := savedRouter(t, true)
	addJob(reports, "rodando", "a@x", "running")
	addJob(reports, "falhou", "a@x", "failed")
	addJob(reports, "do-b", "b@x", "done", rowOf("c", "w"))
	body := map[string]string{"name": "x"}
	if w := chamar(r, http.MethodPost, "/reports/rodando/save", "a@x", body); w.Code != http.StatusConflict {
		t.Errorf("em andamento: %d (esperado 409)", w.Code)
	}
	if w := chamar(r, http.MethodPost, "/reports/falhou/save", "a@x", body); w.Code != http.StatusConflict {
		t.Errorf("que falhou: %d (esperado 409)", w.Code)
	}
	if w := chamar(r, http.MethodPost, "/reports/nao-existe/save", "a@x", body); w.Code != http.StatusNotFound {
		t.Errorf("inexistente ou expirado: %d (esperado 404)", w.Code)
	}
	if w := chamar(r, http.MethodPost, "/reports/do-b/save", "a@x", body); w.Code != http.StatusNotFound {
		t.Errorf("o relatório de outro usuário não existe para este: %d (esperado 404)", w.Code)
	}
}

func TestCadaUsuarioSoAbreEApagaOsSeusRelatoriosSalvos(t *testing.T) {
	r, reports := savedRouter(t, true)
	addJob(reports, "j1", "a@x", "done", rowOf("c", "w"))
	var meta storage.SavedReportMeta
	_ = json.Unmarshal(chamar(r, http.MethodPost, "/reports/j1/save", "a@x", map[string]string{"name": "do A"}).Body.Bytes(), &meta)
	path := fmt.Sprintf("/saved/%d", meta.ID)
	if w := chamar(r, http.MethodGet, path, "b@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("B abriu o relatório do A: %d", w.Code)
	}
	if w := chamar(r, http.MethodDelete, path, "b@x", nil); w.Code != http.StatusNotFound {
		t.Errorf("B apagou o relatório do A: %d", w.Code)
	}
	if w := chamar(r, http.MethodGet, "/saved", "b@x", nil); bytesHave(w.Body.Bytes(), "do A") {
		t.Errorf("a lista do B mostrou o relatório do A: %s", w.Body)
	}
	if w := chamar(r, http.MethodGet, path, "a@x", nil); w.Code != 200 {
		t.Errorf("o relatório do A sumiu: %d", w.Code)
	}
}

func TestNomeInvalidoIdInvalidoELimite(t *testing.T) {
	r, reports := savedRouter(t, true)
	addJob(reports, "j1", "a@x", "done", rowOf("c", "w"))
	if w := chamar(r, http.MethodPost, "/reports/j1/save", "a@x", map[string]string{"name": "   "}); w.Code != http.StatusBadRequest {
		t.Errorf("nome vazio: %d (esperado 400)", w.Code)
	}
	if w := chamar(r, http.MethodPost, "/reports/j1/save", "a@x", "não é objeto"); w.Code != http.StatusBadRequest {
		t.Errorf("corpo inválido: %d (esperado 400)", w.Code)
	}
	for _, id := range []string{"abc", "0", "-3"} {
		if w := chamar(r, http.MethodGet, "/saved/"+id, "a@x", nil); w.Code != http.StatusBadRequest {
			t.Errorf("id %q: %d (esperado 400)", id, w.Code)
		}
	}
	for i := 0; i < storage.MaxSavedReportsPerUser; i++ {
		if w := chamar(r, http.MethodPost, "/reports/j1/save", "a@x", map[string]string{"name": fmt.Sprintf("r%d", i)}); w.Code != http.StatusOK {
			t.Fatalf("relatório %d: %d %s", i, w.Code, w.Body)
		}
	}
	w := chamar(r, http.MethodPost, "/reports/j1/save", "a@x", map[string]string{"name": "um a mais"})
	if w.Code != http.StatusConflict || !bytesHave(w.Body.Bytes(), "apague algum") {
		t.Errorf("no limite: %d %s (esperado 409 dizendo para apagar algum)", w.Code, w.Body)
	}
}

func TestSemArmazenamentoDaServicoIndisponivel(t *testing.T) {
	r, reports := savedRouter(t, false)
	addJob(reports, "j1", "a@x", "done", rowOf("c", "w"))
	for _, c := range []struct{ method, path string }{{http.MethodPost, "/reports/j1/save"}, {http.MethodGet, "/saved"}, {http.MethodGet, "/saved/1"}, {http.MethodDelete, "/saved/1"}} {
		if w := chamar(r, c.method, c.path, "a@x", map[string]string{"name": "x"}); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: %d (esperado 503)", c.method, c.path, w.Code)
		}
	}
}
