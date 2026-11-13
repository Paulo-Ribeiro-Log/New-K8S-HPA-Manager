package storage

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSavedStore(t *testing.T, dir string) *SavedReportsStore {
	t.Helper()
	s, err := NewSavedReportsStore(filepath.Join(dir, "saved-reports-test.db"))
	if err != nil {
		t.Fatalf("NewSavedReportsStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func meta(name string) SavedReportMeta {
	return SavedReportMeta{Name: name, WindowStart: time.Date(2026, 11, 11, 17, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 11, 11, 19, 0, 0, 0, time.UTC), Apps: 13, Clusters: 2}
}

func TestRelatorioSalvoIdaEVoltaComprimido(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	// JSON repetitivo, como um relatório de verdade: comprime bem
	payload := []byte(`{"rows":[` + strings.Repeat(`{"cluster":"aks-a","namespace":"ns","workload":"frete-hub","traffic":{"requests":12345}},`, 500) + `{}]}`)
	m, err := s.Save("a@x.com", meta("  Stress test 11/11  "), payload)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == 0 || m.Name != "Stress test 11/11" || m.SizeBytes != len(payload) || m.CreatedAt.IsZero() {
		t.Fatalf("meta: %+v", m)
	}
	got, raw, err := s.Get("a@x.com", m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, payload) {
		t.Error("o JSON voltou diferente do que foi salvo")
	}
	if got.Name != m.Name || got.Apps != 13 || got.Clusters != 2 || !got.WindowStart.Equal(m.WindowStart) || !got.WindowEnd.Equal(m.WindowEnd) {
		t.Errorf("meta lida: %+v", got)
	}
	// a compressão de fato acontece
	var stored int
	if err := s.db.QueryRow(`SELECT LENGTH(data) FROM saved_reports WHERE id = ?`, m.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored >= len(payload)/4 {
		t.Errorf("guardou %d bytes para um JSON de %d: não comprimiu", stored, len(payload))
	}
}

func TestRelatoriosSalvosSobrevivemAoReinicio(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewSavedReportsStore(filepath.Join(dir, "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := s1.Save("a@x.com", meta("antes do reinício"), []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	s1.Close()
	s2, err := NewSavedReportsStore(filepath.Join(dir, "x.db")) // "reinício": abre o mesmo arquivo
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, raw, err := s2.Get("a@x.com", m.ID); err != nil || string(raw) != `{"ok":true}` {
		t.Fatalf("depois do reinício: %q %v", raw, err)
	}
}

func TestCadaUsuarioSoEnxergaOsSeus(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	a, _ := s.Save("a@x.com", meta("do A"), []byte(`{"u":"a"}`))
	b, _ := s.Save("b@x.com", meta("do B"), []byte(`{"u":"b"}`))
	if l, _ := s.List("a@x.com"); len(l) != 1 || l[0].ID != a.ID {
		t.Errorf("lista do A: %+v", l)
	}
	if _, _, err := s.Get("a@x.com", b.ID); !errors.Is(err, ErrSavedReportNotFound) {
		t.Errorf("A leu o relatório do B: %v", err)
	}
	if err := s.Delete("a@x.com", b.ID); !errors.Is(err, ErrSavedReportNotFound) {
		t.Errorf("A apagou o relatório do B: %v", err)
	}
	if _, _, err := s.Get("b@x.com", b.ID); err != nil {
		t.Errorf("o relatório do B sumiu depois da tentativa do A: %v", err)
	}
	if l, _ := s.List("ninguem@x.com"); l == nil || len(l) != 0 {
		t.Errorf("sem relatórios deve ser lista vazia, não nula: %v", l)
	}
}

func TestListaDoMaisNovoAoMaisAntigoEApagar(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	var ids []int64
	for i := 0; i < 3; i++ {
		m, err := s.Save("a@x.com", meta(fmt.Sprintf("r%d", i)), []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
		time.Sleep(2 * time.Millisecond)
	}
	l, _ := s.List("a@x.com")
	if len(l) != 3 || l[0].ID != ids[2] || l[2].ID != ids[0] {
		t.Fatalf("ordem: %+v", l)
	}
	if err := s.Delete("a@x.com", ids[1]); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.List("a@x.com"); len(l) != 2 {
		t.Errorf("depois de apagar: %d", len(l))
	}
	if err := s.Delete("a@x.com", ids[1]); !errors.Is(err, ErrSavedReportNotFound) {
		t.Errorf("apagar de novo: %v", err)
	}
}

func TestNomeInvalidoVazioOuGrande(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	for _, n := range []string{"", "   ", strings.Repeat("a", MaxSavedReportNameLen+1)} {
		if _, err := s.Save("a@x.com", meta(n), []byte(`{}`)); !errors.Is(err, ErrSavedReportName) {
			t.Errorf("nome %q: %v", n, err)
		}
	}
	if _, err := s.Save("a@x.com", meta(strings.Repeat("ã", MaxSavedReportNameLen)), []byte(`{}`)); err != nil {
		t.Errorf("o limite conta caracteres, não bytes: %v", err)
	}
	if _, err := s.Save("a@x.com", meta("ok"), nil); err == nil {
		t.Error("relatório vazio deveria ser recusado")
	}
}

func TestLimiteDeRelatoriosPorUsuarioENadaESumidoEmSilencio(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	for i := 0; i < MaxSavedReportsPerUser; i++ {
		if _, err := s.Save("a@x.com", meta(fmt.Sprintf("r%d", i)), []byte(`{}`)); err != nil {
			t.Fatalf("relatório %d: %v", i, err)
		}
	}
	if _, err := s.Save("a@x.com", meta("um a mais"), []byte(`{}`)); !errors.Is(err, ErrSavedReportsLimit) {
		t.Fatalf("o %dº deveria ser recusado: %v", MaxSavedReportsPerUser+1, err)
	}
	if l, _ := s.List("a@x.com"); len(l) != MaxSavedReportsPerUser || l[len(l)-1].Name != "r0" {
		t.Errorf("nada deve ter sido descartado: %d relatórios, o mais antigo %q", len(l), l[len(l)-1].Name)
	}
	// o limite é por usuário
	if _, err := s.Save("b@x.com", meta("do B"), []byte(`{}`)); err != nil {
		t.Errorf("outro usuário não é afetado pelo limite: %v", err)
	}
	// apagar libera espaço
	l, _ := s.List("a@x.com")
	_ = s.Delete("a@x.com", l[0].ID)
	if _, err := s.Save("a@x.com", meta("agora cabe"), []byte(`{}`)); err != nil {
		t.Errorf("depois de apagar deveria caber: %v", err)
	}
}

func TestRelatorioGrandeDemaisERecusado(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	if _, err := s.Save("a@x.com", meta("gigante"), bytes.Repeat([]byte("a"), MaxSavedReportBytes+1)); !errors.Is(err, ErrSavedReportTooLarge) {
		t.Errorf("deveria recusar: %v", err)
	}
	if _, err := s.Save("a@x.com", meta("no limite"), bytes.Repeat([]byte("a"), MaxSavedReportBytes)); err != nil {
		t.Errorf("exatamente no limite deve caber: %v", err)
	}
}

func TestRelatorioCorrompidoNoBancoDaErroEmVezDePanico(t *testing.T) {
	s := newSavedStore(t, t.TempDir())
	m, _ := s.Save("a@x.com", meta("x"), []byte(`{}`))
	if _, err := s.db.Exec(`UPDATE saved_reports SET data = ? WHERE id = ?`, []byte("não é gzip"), m.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("a@x.com", m.ID); err == nil || !strings.Contains(err.Error(), "descomprimir") {
		t.Errorf("deveria acusar o dado corrompido: %v", err)
	}
}
