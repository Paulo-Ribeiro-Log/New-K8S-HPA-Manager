package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"k8s-hpa-manager/internal/history"
)

// O manifesto do CronJob pode ter env com segredo: o histórico (somente-inclusão) guarda só hash e tamanho.
func TestRecordBatchDeleteNaoGravaManifesto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ht, err := history.NewHistoryTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistoryTracker: %v", err)
	}
	t.Cleanup(func() { ht.Close() })

	h := &CronJobHandler{historyTracker: ht}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("DELETE", "/", nil)
	manifest := "kind: CronJob\nenv:\n- name: DB_PASSWORD\n  value: super-secreto\n"
	h.recordBatchDelete(c, "delete_cronjob", "ns/cj", "cl", manifest, nil, time.Now(), nil)

	entries, _, err := ht.Query(history.HistoryFilter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entradas=%d err=%v", len(entries), err)
	}
	b := entries[0].Before
	if _, ok := b["yaml"]; ok {
		t.Fatal("o campo yaml não deve existir")
	}
	for k, v := range b {
		if s, ok := v.(string); ok && strings.Contains(s, "super-secreto") {
			t.Fatalf("campo %q guarda o manifesto", k)
		}
	}
	if b["yaml_sha256"] == nil || b["yaml_bytes"] == nil {
		t.Fatalf("faltou identificação: %v", b)
	}
}
