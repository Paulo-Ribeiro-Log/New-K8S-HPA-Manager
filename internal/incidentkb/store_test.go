package incidentkb

import (
	"strings"
	"testing"
)

func TestSaveListSearch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	inc := &Incident{
		Author:       "analista@example.com",
		Cluster:      "akspriv-entregamais-prd",
		Namespace:    "entrega-mais-prd",
		ResourceType: "deployment",
		ResourceName: "entrega-mais-bff",
		Severity:     "high",
		Tags:         []string{"crashloop", "certificado"},
		Symptom:      "Pods em CrashLoopBackOff após deploy",
		RootCause:    "Certificado TLS do webhook dsv-injector expirado",
		Resolution:   "Renovado o certificado via cert-manager e reiniciado o webhook",
	}

	saved, err := store.Save(inc)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("esperava ID gerado")
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("esperava 1 incidente, veio %d", len(all))
	}
	if all[0].Resolution != inc.Resolution {
		t.Errorf("Resolution não sobreviveu ao round-trip: %q", all[0].Resolution)
	}

	got, err := store.GetByID(saved.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("esperava encontrar incidente por ID")
	}

	results, err := store.Search("certificado", SearchFilters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("esperava 1 resultado pra 'certificado', veio %d", len(results))
	}

	noResults, err := store.Search("inexistente-xyz", SearchFilters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(noResults) != 0 {
		t.Fatalf("esperava 0 resultados, veio %d", len(noResults))
	}
}

func TestExportMarkdown(t *testing.T) {
	inc := &Incident{
		ID:           "abc123",
		Cluster:      "akspriv-entregamais-prd",
		Namespace:    "entrega-mais-prd",
		ResourceType: "deployment",
		ResourceName: "entrega-mais-bff",
		Severity:     "high",
		Tags:         []string{"crashloop", "certificado"},
		Symptom:      "Pods em CrashLoopBackOff após deploy",
		RootCause:    "Certificado TLS do webhook dsv-injector expirado",
		Resolution:   "Renovado o certificado via cert-manager",
	}

	single := string(ExportMarkdown(inc))
	if strings.Contains(single, "---\nid:") {
		t.Error("export não deveria conter front-matter YAML bruto")
	}
	for _, want := range []string{"# akspriv-entregamais-prd", "| Severidade | high |", "## Sintoma", "## Causa raiz", "## Resolução", "Renovado o certificado"} {
		if !strings.Contains(single, want) {
			t.Errorf("export de incidente único deveria conter %q", want)
		}
	}

	bundle := string(ExportBundleMarkdown([]*Incident{inc, inc}))
	if !strings.Contains(bundle, "Base de Conhecimento de Incidentes (2 registros)") {
		t.Error("export em lote deveria ter o título com a contagem")
	}
	if strings.Count(bundle, "### Sintoma") != 2 {
		t.Error("export em lote deveria ter subseções de nível 3 (um nível abaixo do título ## por incidente)")
	}
}
