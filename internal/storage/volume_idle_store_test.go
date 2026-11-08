package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestVolumeIdleStoreSync(t *testing.T) {
	s, err := NewVolumeIdleStore(filepath.Join(t.TempDir(), "volume_idle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	got, err := s.Sync("c1", []string{"a", "b"}, t0)
	if err != nil || len(got) != 2 || !got["a"].Equal(t0) {
		t.Fatalf("1ª leitura: %v %v", got, err)
	}
	// 10 dias depois: "a" continua ocioso (mantém first_seen), "b" voltou a ser usado, "c" é novo
	t1 := t0.AddDate(0, 0, 10)
	got, err = s.Sync("c1", []string{"a", "c"}, t1)
	if err != nil {
		t.Fatal(err)
	}
	if !got["a"].Equal(t0) || !got["c"].Equal(t1) {
		t.Fatalf("first_seen: %v", got)
	}
	if _, ok := got["b"]; ok {
		t.Fatalf("b deveria ter saído: %v", got)
	}
	// outro cluster não é afetado
	if o, _ := s.Sync("c2", []string{"x"}, t1); len(o) != 1 {
		t.Fatalf("c2: %v", o)
	}
	if again, _ := s.Sync("c1", nil, t1.Add(time.Hour)); len(again) != 0 {
		t.Fatalf("lista vazia deveria limpar c1: %v", again)
	}
	if o, _ := s.Sync("c2", []string{"x"}, t1.Add(time.Hour)); !o["x"].Equal(t1) {
		t.Fatalf("c2 perdeu first_seen: %v", o)
	}
}
