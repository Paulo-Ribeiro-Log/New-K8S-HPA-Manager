package promwal

import (
	"strings"
	"testing"
	"time"
)

func TestBuildRecurrence(t *testing.T) {
	now := testNow
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour

	t.Run("a mesma limpeza nas duas fontes conta uma vez", func(t *testing.T) {
		r := BuildRecurrence([]time.Time{ago(2 * day)}, []time.Time{ago(2*day - 2*time.Minute)}, 0, now)
		if r.Count30d != 1 || r.Recurrent {
			t.Fatalf("esperava 1 e não recorrente: %+v", r)
		}
	})
	t.Run("duas em 30 dias = recorrente", func(t *testing.T) {
		r := BuildRecurrence([]time.Time{ago(3 * day)}, []time.Time{ago(10 * day)}, 0, now)
		if r.Count30d != 2 || !r.Recurrent || r.LastAt == nil || !r.LastAt.Equal(ago(3*day)) {
			t.Fatalf("%+v", r)
		}
		if r.Dates[0].Before(r.Dates[1]) {
			t.Error("datas deveriam vir da mais recente para a mais antiga")
		}
	})
	t.Run("fora da janela não conta, mas entra no total", func(t *testing.T) {
		r := BuildRecurrence([]time.Time{ago(40 * day), ago(2 * day)}, nil, 7, now)
		if r.Count30d != 1 || r.Recurrent || r.Total != 7 {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("sem nada", func(t *testing.T) {
		r := BuildRecurrence(nil, nil, 0, now)
		if r.Count30d != 0 || r.Total != 0 || r.LastAt != nil || r.Recurrent {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("limpezas feitas por outros só na anotação já bastam", func(t *testing.T) {
		r := BuildRecurrence([]time.Time{ago(day), ago(5 * day)}, nil, 2, now)
		if !r.Recurrent {
			t.Fatalf("%+v", r)
		}
	})
}

func TestCleanupAnnotationsLimitaHistorico(t *testing.T) {
	cur := map[string]string{AnnotationCleanupCount: "11"}
	var dates []string
	for i := 0; i < 10; i++ {
		dates = append(dates, testNow.Add(-time.Duration(10-i)*time.Hour).UTC().Format(time.RFC3339))
	}
	cur[AnnotationCleanupHistory] = strings.Join(dates, ",")

	got := cleanupAnnotations(cur, testNow)
	if got[AnnotationCleanupCount] != "12" {
		t.Errorf("contador = %v", got[AnnotationCleanupCount])
	}
	parsed := parseDates(got[AnnotationCleanupHistory].(string))
	if len(parsed) != maxHistoryDates || !parsed[len(parsed)-1].Equal(testNow) {
		t.Errorf("histórico deveria manter só as %d últimas, terminando em now: %d", maxHistoryDates, len(parsed))
	}
	if parseCount("lixo") != 0 || len(parseDates("x,,y")) != 0 {
		t.Error("valores ilegíveis devem ser ignorados")
	}
}

func TestAnaliseAvisaReincidencia(t *testing.T) {
	in := Input{
		MemoryLimitBytes: 2 * gi,
		Pods:             []PodInfo{oomPod("p-0", replayLog)},
		CleanupDates:     []time.Time{testNow.Add(-72 * time.Hour)},
		HistoryCleanups:  []time.Time{testNow.Add(-26 * time.Hour)},
	}
	r := Analyze(in, testNow)
	if !r.Recurrence.Recurrent || r.Recurrence.Count30d != 2 {
		t.Fatalf("recorrência = %+v", r.Recurrence)
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "2 vezes nos últimos 30 dias") {
		t.Errorf("faltou o aviso de reincidência: %v", r.Warnings)
	}
	// Uma limpeza só: sem aviso.
	in.CleanupDates = nil
	if r := Analyze(in, testNow); r.Recurrence.Recurrent || strings.Contains(strings.Join(r.Warnings, ""), "vezes nos últimos") {
		t.Errorf("1 limpeza não é reincidência: %+v %v", r.Recurrence, r.Warnings)
	}
	// O aviso também aparece com limpeza pendente (retorno antecipado da análise).
	in.CleanupDates = []time.Time{testNow.Add(-72 * time.Hour)}
	in.PendingRestore, in.PendingRestoreReplicas = true, 2
	if r := Analyze(in, testNow); len(r.Warnings) == 0 {
		t.Error("o aviso de reincidência deveria aparecer mesmo com restauração pendente")
	}
}
