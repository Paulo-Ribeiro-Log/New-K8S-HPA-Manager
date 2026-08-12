package teams

import (
	"testing"
	"time"
)

func TestFilterByAge(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-7 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-10 * 24 * time.Hour)

	items := []ApprovalItem{
		{CHG: "CHG_RECENT", PostedAt: &recent},
		{CHG: "CHG_OLD", PostedAt: &old},
		{CHG: "CHG_NO_TIMESTAMP"}, // PostedAt nil — deve ser mantido
	}

	got := filterByAge(items, cutoff)

	if len(got) != 2 {
		t.Fatalf("esperado 2 itens, got %d: %+v", len(got), got)
	}
	chgs := map[string]bool{}
	for _, i := range got {
		chgs[i.CHG] = true
	}
	if !chgs["CHG_RECENT"] {
		t.Error("CHG_RECENT deveria ter sido mantido")
	}
	if !chgs["CHG_NO_TIMESTAMP"] {
		t.Error("CHG_NO_TIMESTAMP (sem PostedAt) deveria ter sido mantido")
	}
	if chgs["CHG_OLD"] {
		t.Error("CHG_OLD deveria ter sido descartado por estar fora da janela")
	}
}

func TestFilterByAge_EmptyInput(t *testing.T) {
	got := filterByAge(nil, time.Now())
	if len(got) != 0 {
		t.Fatalf("esperado slice vazio, got %d", len(got))
	}
}

func TestCutoffTime_UsesBusinessDays(t *testing.T) {
	// Segunda-feira, 17/ago/2026 (confirmado via `date -d`) — semana sem feriado nacional.
	monday := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	got := CutoffTime(monday)
	// 3 dias úteis atrás de segunda: sexta, quinta, quarta -> quarta-feira 12/ago/2026, não
	// "3 dias corridos atrás" (que cairia em sexta-feira 14/ago).
	want := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("esperado %v, got %v", want, got)
	}
}
