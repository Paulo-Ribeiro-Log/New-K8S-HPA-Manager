package healthcheck

import (
	"path/filepath"
	"testing"
)

func newTestTriageIgnoreManager(t *testing.T) *TriageIgnoreManager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "triage_ignore.json")
	mgr, err := NewTriageIgnoreManager(path)
	if err != nil {
		t.Fatalf("NewTriageIgnoreManager: %v", err)
	}
	return mgr
}

func TestTriageIgnoreManager_AddEntry_ValidationAndDedup(t *testing.T) {
	mgr := newTestTriageIgnoreManager(t)

	// Fonte inválida deve ser rejeitada.
	if err := mgr.AddEntry(TriageIgnoreEntry{Source: "not-a-real-source", Value: "x"}); err == nil {
		t.Fatalf("esperava erro para fonte inválida")
	}

	// Value vazio deve ser rejeitado.
	if err := mgr.AddEntry(TriageIgnoreEntry{Source: TriageIgnoreSourcePrometheusAlert, Value: ""}); err == nil {
		t.Fatalf("esperava erro para value vazio")
	}

	// Entrada válida deve funcionar.
	if err := mgr.AddEntry(TriageIgnoreEntry{ID: "1", Source: TriageIgnoreSourcePrometheusAlert, Value: "Watchdog"}); err != nil {
		t.Fatalf("AddEntry válida falhou: %v", err)
	}

	// Duplicata exata (mesma Source+Value) deve ser rejeitada.
	if err := mgr.AddEntry(TriageIgnoreEntry{ID: "2", Source: TriageIgnoreSourcePrometheusAlert, Value: "Watchdog"}); err == nil {
		t.Fatalf("esperava erro para entrada duplicada")
	}

	// Mesmo Value em Source diferente NÃO é duplicata.
	if err := mgr.AddEntry(TriageIgnoreEntry{ID: "3", Source: TriageIgnoreSourceDynatraceProblem, Value: "Watchdog"}); err != nil {
		t.Fatalf("mesma Value em Source diferente deveria ser permitida: %v", err)
	}

	entries := mgr.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("esperava 2 entradas — veio %d", len(entries))
	}
}

func TestTriageIgnoreManager_RemoveEntry(t *testing.T) {
	mgr := newTestTriageIgnoreManager(t)
	if err := mgr.AddEntry(TriageIgnoreEntry{ID: "abc", Source: TriageIgnoreSourcePrometheusAlert, Value: "Watchdog"}); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := mgr.RemoveEntry("does-not-exist"); err == nil {
		t.Fatalf("esperava erro ao remover ID inexistente")
	}

	if err := mgr.RemoveEntry("abc"); err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}

	if len(mgr.GetEntries()) != 0 {
		t.Fatalf("esperava 0 entradas após remover a única existente")
	}
}

// TestTriageIgnoreManager_IgnoredValues cobre o ponto que os TargetSource concretos
// (target_source_dynatrace.go, target_source_prometheus.go) realmente consomem: o mapa
// devolvido só deve conter valores da fonte pedida, nunca vazar entre fontes diferentes.
func TestTriageIgnoreManager_IgnoredValues(t *testing.T) {
	mgr := newTestTriageIgnoreManager(t)
	mustAdd(t, mgr, TriageIgnoreEntry{ID: "1", Source: TriageIgnoreSourcePrometheusAlert, Value: "Watchdog"})
	mustAdd(t, mgr, TriageIgnoreEntry{ID: "2", Source: TriageIgnoreSourcePrometheusAlert, Value: "InfoInhibitor"})
	mustAdd(t, mgr, TriageIgnoreEntry{ID: "3", Source: TriageIgnoreSourceDynatraceProblem, Value: "Known flaky check"})

	promIgnored := mgr.IgnoredValues(TriageIgnoreSourcePrometheusAlert)
	if len(promIgnored) != 2 {
		t.Fatalf("esperava 2 valores ignorados para Prometheus — veio %d", len(promIgnored))
	}
	if _, ok := promIgnored["Watchdog"]; !ok {
		t.Errorf("esperava 'Watchdog' no conjunto de ignorados do Prometheus")
	}
	if _, ok := promIgnored["Known flaky check"]; ok {
		t.Errorf("entrada do Dynatrace vazou pro conjunto do Prometheus")
	}

	dtIgnored := mgr.IgnoredValues(TriageIgnoreSourceDynatraceProblem)
	if len(dtIgnored) != 1 {
		t.Fatalf("esperava 1 valor ignorado para Dynatrace — veio %d", len(dtIgnored))
	}

	// Fonte sem nenhuma entrada cadastrada retorna mapa vazio, não nil-panicking em lookup.
	zabbixIgnored := mgr.IgnoredValues(TriageIgnoreSourceZabbixTrigger)
	if len(zabbixIgnored) != 0 {
		t.Fatalf("esperava 0 valores ignorados para Zabbix — veio %d", len(zabbixIgnored))
	}
}

// TestTriageIgnoreManager_PersistsAcrossReload confirma que Save/Load (o mesmo padrão de
// persistência do FilterManager) realmente sobrevive a um reload do zero — não só em memória.
func TestTriageIgnoreManager_PersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "triage_ignore.json")

	mgr1, err := NewTriageIgnoreManager(path)
	if err != nil {
		t.Fatalf("NewTriageIgnoreManager: %v", err)
	}
	mustAdd(t, mgr1, TriageIgnoreEntry{ID: "1", Source: TriageIgnoreSourcePrometheusAlert, Value: "Watchdog"})
	if err := mgr1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	mgr2, err := NewTriageIgnoreManager(path)
	if err != nil {
		t.Fatalf("NewTriageIgnoreManager (reload): %v", err)
	}
	entries := mgr2.GetEntries()
	if len(entries) != 1 || entries[0].Value != "Watchdog" {
		t.Fatalf("esperava a entrada persistida sobreviver ao reload — veio %+v", entries)
	}
}

func mustAdd(t *testing.T, mgr *TriageIgnoreManager, entry TriageIgnoreEntry) {
	t.Helper()
	if err := mgr.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry(%+v): %v", entry, err)
	}
}
