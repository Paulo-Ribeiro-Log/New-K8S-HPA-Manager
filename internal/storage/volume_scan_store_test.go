package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestVolumeScanStore(t *testing.T) {
	s, err := NewVolumeScanStore(filepath.Join(t.TempDir(), "volume_scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWatched([]string{"b", "a", "a", ""}); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.Watched(); len(w) != 2 || w[0] != "a" || w[1] != "b" {
		t.Fatalf("watched = %v", w)
	}
	at := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	for _, c := range []string{"a", "b"} {
		if err := s.SaveResult(c, at, `{"x":1}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveResult("a", at.Add(time.Hour), `{"x":2}`); err != nil { // substitui
		t.Fatal(err)
	}
	r, _ := s.Results()
	if len(r) != 2 || r["a"].Payload != `{"x":2}` || !r["a"].ScannedAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("results = %+v", r)
	}
	// tirar "b" da lista apaga o resultado dele
	if err := s.SetWatched([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Results(); len(r) != 1 || r["b"].Payload != "" {
		t.Fatalf("b deveria ter sido apagado: %+v", r)
	}
}
