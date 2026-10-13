package history

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newTestTracker(t *testing.T) (*HistoryTracker, string) {
	t.Helper()
	dir := t.TempDir()
	ht, err := NewHistoryTracker(dir)
	if err != nil {
		t.Fatalf("NewHistoryTracker: %v", err)
	}
	t.Cleanup(func() { ht.Close() })
	return ht, dir
}

func TestLogQueryAndFilters(t *testing.T) {
	ht, _ := newTestTracker(t)
	ctx := WithUser(context.Background(), UserInfo{Email: "ana@corp", Name: "Ana"})
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	for i := 0; i < 5; i++ {
		status := StatusSuccess
		if i == 4 {
			status = "error" // legado: deve virar "failed"
		}
		if err := ht.Log(ctx, HistoryEntry{
			Timestamp: base.Add(time.Duration(i) * time.Hour),
			Action:    "update_hpa",
			Resource:  fmt.Sprintf("ns/App-%d", i),
			Cluster:   "aks-a",
			Status:    status,
			Before:    map[string]interface{}{"min": i},
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = ht.Log(context.Background(), HistoryEntry{Timestamp: base.AddDate(0, 0, -1), Action: "scale_deployment", Cluster: "aks-b", Status: "success"})

	all, total, err := ht.Query(HistoryFilter{})
	if err != nil || total != 6 || len(all) != 6 {
		t.Fatalf("Query sem filtro: total=%d len=%d err=%v", total, len(all), err)
	}
	if all[0].Resource != "ns/App-4" {
		t.Errorf("ordem: primeiro=%q, quer o mais recente", all[0].Resource)
	}
	if all[0].Status != StatusFailed {
		t.Errorf("status legado 'error' gravado como %q, quer failed", all[0].Status)
	}
	if all[0].UserEmail != "ana@corp" || all[0].UserName != "Ana" {
		t.Errorf("autor do ctx não gravado: %+v", all[0])
	}
	if all[1].Before["min"] != float64(3) {
		t.Errorf("before não voltou do JSON: %#v", all[1].Before)
	}

	page, total, _ := ht.Query(HistoryFilter{Limit: 2, Offset: 2})
	if total != 6 || len(page) != 2 || page[0].Resource != "ns/App-2" {
		t.Errorf("paginação: total=%d len=%d primeiro=%q", total, len(page), page[0].Resource)
	}

	cases := []struct {
		name string
		f    HistoryFilter
		want int
	}{
		{"action", HistoryFilter{Action: "scale_deployment"}, 1},
		{"actions", HistoryFilter{Actions: []string{"scale_deployment", "update_hpa"}}, 6},
		{"cluster", HistoryFilter{Cluster: "aks-a"}, 5},
		{"resource parcial sem caixa", HistoryFilter{Resource: "app-1"}, 1},
		{"status failed inclui legado", HistoryFilter{Status: "failed"}, 1},
		{"status error normalizado", HistoryFilter{Status: "error"}, 1},
		{"usuário", HistoryFilter{UserEmail: "ana@corp"}, 5},
		{"dia exato", HistoryFilter{StartDate: base, EndDate: base}, 5},
		{"dia anterior", HistoryFilter{EndDate: base.AddDate(0, 0, -1)}, 1},
	}
	for _, tc := range cases {
		if _, n, err := ht.Query(tc.f); err != nil || n != tc.want {
			t.Errorf("%s: total=%d quer %d (err=%v)", tc.name, n, tc.want, err)
		}
	}

	stats, err := ht.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 6 || stats.Success != 5 || stats.Failed != 1 || stats.ByUser["ana@corp"] != 5 || stats.ByCluster["aks-b"] != 1 {
		t.Errorf("stats: %+v", stats)
	}

	got, err := ht.GetByID(all[0].ID)
	if err != nil || got.Resource != "ns/App-4" {
		t.Errorf("GetByID: %+v %v", got, err)
	}
	if _, err := ht.GetByID("nao-existe"); err == nil {
		t.Error("GetByID de id inexistente deveria falhar")
	}
}

func TestUserFromGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)

	// Middleware JWT: chaves do gin + request ctx
	c.Set("user_email", "bia@corp")
	if u := UserFromContext(c); u.Email != "bia@corp" {
		t.Errorf("gin keys: %+v", u)
	}
	c.Request = c.Request.WithContext(WithUser(c.Request.Context(), UserInfo{Email: "req@corp", Name: "Req"}))
	if u := UserFromContext(c.Request.Context()); u.Email != "req@corp" {
		t.Errorf("request ctx: %+v", u)
	}
	// Goroutine após a requisição: o ctx destacado guarda o autor e não é cancelado com ela
	reqCtx, cancel := context.WithCancel(c.Request.Context())
	detached := DetachedUserContext(reqCtx)
	cancel()
	if detached.Err() != nil || UserFromContext(detached).Email != "req@corp" {
		t.Errorf("DetachedUserContext: err=%v user=%+v", detached.Err(), UserFromContext(detached))
	}
	if u := UserFromContext(context.Background()); u.Email != "" {
		t.Errorf("ctx vazio deveria dar usuário vazio: %+v", u)
	}
}

func TestEntriesAreImmutableWithinRetention(t *testing.T) {
	ht, _ := newTestTracker(t)
	_ = ht.Log(context.Background(), HistoryEntry{ID: "x1", Action: "delete_pod", Status: "success"})
	if _, err := ht.db.Exec(`UPDATE history_entries SET action = 'nada' WHERE id = 'x1'`); err == nil {
		t.Error("UPDATE deveria ser recusado pelo trigger")
	}
	if _, err := ht.db.Exec(`DELETE FROM history_entries WHERE id = 'x1'`); err == nil {
		t.Error("DELETE de entrada recente deveria ser recusado pelo trigger")
	}
	if err := ht.Log(context.Background(), HistoryEntry{ID: "x1", Action: "outra"}); err == nil {
		t.Error("id duplicado deveria falhar (sem sobrescrever)")
	}
}

func TestPurgeExpired(t *testing.T) {
	ht, _ := newTestTracker(t)
	old := time.Now().AddDate(0, 0, -RetentionDays-2)
	_ = ht.Log(context.Background(), HistoryEntry{ID: "velha", Timestamp: old, Action: "a"})
	_ = ht.Log(context.Background(), HistoryEntry{ID: "nova", Action: "a"})
	n, err := ht.PurgeExpired()
	if err != nil || n != 1 {
		t.Fatalf("PurgeExpired: n=%d err=%v", n, err)
	}
	if _, err := ht.GetByID("nova"); err != nil {
		t.Error("entrada recente não pode ser apagada")
	}
}

func TestRetentionTriggerMatchesConst(t *testing.T) {
	if retentionDaysSQL != strconv.Itoa(RetentionDays) {
		t.Fatalf("retentionDaysSQL=%s diferente de RetentionDays=%d", retentionDaysSQL, RetentionDays)
	}
}

func TestImportLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	month := filepath.Join(dir, "history", "2026-09")
	if err := os.MkdirAll(month, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, e HistoryEntry) {
		b, _ := json.Marshal(e)
		if err := os.WriteFile(filepath.Join(month, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	ts := time.Now().AddDate(0, 0, -10)
	write("a.json", HistoryEntry{ID: "a", Timestamp: ts, Action: "kill_pod", Cluster: "aks", Status: "error", UserEmail: "x@corp"})
	write("b.json", HistoryEntry{ID: "b", Timestamp: ts.Add(time.Minute), Action: "resync_akv", Status: "success", After: map[string]interface{}{"k": "v"}})
	_ = os.WriteFile(filepath.Join(month, "quebrado.json"), []byte("{"), 0644)

	ht, err := NewHistoryTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, total, _ := ht.Query(HistoryFilter{})
	if total != 2 {
		t.Fatalf("importadas=%d, quer 2", total)
	}
	if entries[1].Status != StatusFailed || entries[1].UserEmail != "x@corp" || entries[0].After["k"] != "v" {
		t.Errorf("campos importados errados: %+v", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "history")); !os.IsNotExist(err) {
		t.Error("pasta history/ deveria ter virado history-json-backup/")
	}
	info, err := os.Stat(filepath.Join(dir, "history-json-backup", "2026-09", "a.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("backup com permissão %v (err=%v), quer 0600", info.Mode().Perm(), err)
	}
	ht.Close()

	// Reabrir não reimporta nem duplica
	ht2, err := NewHistoryTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ht2.Close()
	if _, total, _ := ht2.Query(HistoryFilter{}); total != 2 {
		t.Errorf("reabertura duplicou: total=%d", total)
	}
}

func TestDatabasePermissions(t *testing.T) {
	ht, dir := newTestTracker(t)
	_ = ht.Log(context.Background(), HistoryEntry{Action: "a"})
	for _, name := range []string{"history.db", "history.db-wal"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue // -wal pode já ter sido checkpointed
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s com permissão %v, quer 0600", name, info.Mode().Perm())
		}
	}
}

func TestNilTrackerIsSafe(t *testing.T) {
	var ht *HistoryTracker
	if err := ht.Log(context.Background(), HistoryEntry{Action: "a"}); err != nil {
		t.Error(err)
	}
	if e, n, err := ht.Query(HistoryFilter{}); err != nil || n != 0 || e == nil {
		t.Errorf("Query nil: %v %d %v", e, n, err)
	}
	if _, err := ht.Stats(); err != nil {
		t.Error(err)
	}
	ht.StartRetention(context.Background())
}

func TestNormalizeStatus(t *testing.T) {
	for in, want := range map[string]string{"": "", "success": "success", "OK": "success", "error": "failed", "failed": "failed", "partial": "partial", "running": "running"} {
		if got := NormalizeStatus(in); got != want {
			t.Errorf("NormalizeStatus(%q)=%q quer %q", in, got, want)
		}
	}
}

// BenchmarkLog e BenchmarkQueryPage medem o custo real do SQLite com 100 mil entradas
// (go test -bench . -run ^$ ./internal/history/).
func BenchmarkLog(b *testing.B) {
	ht, err := NewHistoryTracker(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer ht.Close()
	ctx := WithUser(context.Background(), UserInfo{Email: "bench@corp"})
	entry := HistoryEntry{Action: "update_hpa", Resource: "ns/app", Cluster: "aks", Status: "success",
		Before: map[string]interface{}{"min": 2, "max": 10}, After: map[string]interface{}{"min": 3, "max": 12}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.ID = ""
		if err := ht.Log(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueryPage(b *testing.B) {
	ht, err := NewHistoryTracker(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer ht.Close()
	tx, _ := ht.db.Begin()
	now := time.Now()
	for i := 0; i < 100000; i++ {
		_, _ = tx.Exec(`INSERT INTO history_entries (id, ts, user_email, action, resource, cluster, status, before_json, after_json)
			VALUES (?, ?, 'u@corp', ?, ?, ?, 'success', '{"min":2}', '{"min":3}')`,
			strconv.Itoa(i), now.Add(-time.Duration(i)*time.Minute).UnixMilli(), fmt.Sprintf("act_%d", i%20), fmt.Sprintf("ns/app-%d", i), fmt.Sprintf("aks-%d", i%8))
	}
	_ = tx.Commit()
	b.Run("pagina_200_sem_filtro", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, _, err := ht.Query(HistoryFilter{Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("pagina_200_por_cluster", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, _, err := ht.Query(HistoryFilter{Cluster: "aks-3", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("busca_recurso_parcial", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, _, err := ht.Query(HistoryFilter{Resource: "app-9999", Limit: 200}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("stats", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := ht.Stats(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
