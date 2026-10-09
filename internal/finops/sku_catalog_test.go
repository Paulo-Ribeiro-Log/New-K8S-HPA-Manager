package finops

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const skuListFixture = `[
 {"resourceType":"virtualMachines","name":"Standard_D4s_v5",
  "locationInfo":[{"location":"brazilsouth","zones":["2","1","3"]}],
  "capabilities":[{"name":"vCPUs","value":"4"},{"name":"MemoryGB","value":"16"},{"name":"vCPUsPerCore","value":"2"},{"name":"EphemeralOSDiskSupported","value":"False"}],
  "restrictions":[]},
 {"resourceType":"virtualMachines","name":"Standard_F4as_v6",
  "locationInfo":[{"location":"brazilsouth","zones":["1","2","3"]}],
  "capabilities":[{"name":"vCPUs","value":"4"},{"name":"MemoryGB","value":"16"},{"name":"vCPUsPerCore","value":"1"},{"name":"EphemeralOSDiskSupported","value":"True"}],
  "restrictions":[{"type":"Zone","reasonCode":"NotAvailableForSubscription","restrictionInfo":{"zones":["3"]}}]},
 {"resourceType":"virtualMachines","name":"Standard_E4as_v5",
  "locationInfo":[{"location":"brazilsouth","zones":["1"]}],
  "capabilities":[{"name":"vCPUs","value":"4"}],
  "restrictions":[{"type":"Location","reasonCode":"NotAvailableForSubscription","restrictionInfo":{"zones":[]}}]},
 {"resourceType":"disks","name":"Premium_LRS","locationInfo":[],"capabilities":[],"restrictions":[]},
 {"resourceType":"virtualMachines","name":"Basic_A1","locationInfo":[],"capabilities":[],"restrictions":[]}
]`

func TestParseAzureSKUList(t *testing.T) {
	m, err := ParseAzureSKUList([]byte(skuListFixture), "brazilsouth")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 {
		t.Fatalf("SKUs = %d; quer 3 (só virtualMachines Standard_*)", len(m))
	}
	d := m["standard_d4s_v5"]
	if d.VCPU != 4 || d.MemGB != 16 || d.VCPUsPerCore != 2 || d.EphemeralOSDisk || strings.Join(d.Zones, ",") != "1,2,3" {
		t.Errorf("D4s_v5 = %+v", d)
	}
	if f := m["standard_f4as_v6"]; !f.EphemeralOSDisk || f.VCPUsPerCore != 1 || strings.Join(f.Zones, ",") != "1,2" {
		t.Errorf("F4as_v6 (zona 3 restrita) = %+v", f)
	}
	if e := m["standard_e4as_v5"]; !e.Restricted || e.RestrictionReason != "NotAvailableForSubscription" {
		t.Errorf("E4as_v5 = %+v", e)
	}
	if _, err := ParseAzureSKUList([]byte("não é json"), "brazilsouth"); err == nil {
		t.Error("JSON inválido deveria dar erro")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("tempo esgotado esperando a atualização em segundo plano")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSKUCatalogStore(t *testing.T) {
	dir := t.TempDir()
	var calls int32
	s := NewSKUCatalogStore(dir)
	s.runner = func(ctx context.Context, region string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(skuListFixture), nil
	}

	cat, status, _ := s.Get("brazilsouth")
	if cat != nil || status != SKUCatalogLoading {
		t.Fatalf("primeira chamada: cat=%v status=%s; quer nil/loading", cat, status)
	}
	waitFor(t, func() bool { c, st, _ := s.Get("brazilsouth"); return c != nil && st == SKUCatalogFresh })
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("runner chamado %d vezes; quer 1", calls)
	}
	if _, err := os.Stat(s.file("brazilsouth")); err != nil {
		t.Errorf("catálogo não foi gravado em disco: %v", err)
	}

	// Outro store lê do disco, sem chamar o az.
	s2 := NewSKUCatalogStore(dir)
	s2.runner = func(ctx context.Context, region string) ([]byte, error) {
		t.Error("não deveria chamar o az com cache em disco válido")
		return nil, errors.New("x")
	}
	if c, st, _ := s2.Get("brazilsouth"); c == nil || st != SKUCatalogFresh || len(c.SKUs) != 3 {
		t.Errorf("leitura do disco: status=%s", st)
	}

	// Vencido: devolve o que tem e atualiza em segundo plano.
	s2.mu.Lock()
	s2.mem["brazilsouth"].FetchedAt = time.Now().Add(-8 * 24 * time.Hour)
	s2.runner = func(ctx context.Context, region string) ([]byte, error) { return []byte(skuListFixture), nil }
	s2.mu.Unlock()
	if c, st, _ := s2.Get("brazilsouth"); c == nil || st != SKUCatalogStale {
		t.Errorf("vencido: status=%s", st)
	}
	waitFor(t, func() bool { _, st, _ := s2.Get("brazilsouth"); return st == SKUCatalogFresh })

	// Região inválida e erro do az.
	if _, st, _ := s.Get("brazil south; rm"); st != SKUCatalogUnavailable {
		t.Errorf("região inválida: status=%s", st)
	}
	s3 := NewSKUCatalogStore(t.TempDir())
	s3.runner = func(ctx context.Context, region string) ([]byte, error) {
		return nil, errors.New("az CLI não encontrado")
	}
	s3.Get("eastus")
	waitFor(t, func() bool { _, _, e := s3.Get("eastus"); return e != "" })
}

func TestApplySKUCaps(t *testing.T) {
	caps, _ := ParseAzureSKUList([]byte(skuListFixture), "brazilsouth")
	in := DeepAnalysisInput{SKUCaps: caps}
	ov := DeepPoolOverview{OSDisk: "ephemeral", Zones: []string{"brazilsouth-2", "brazilsouth-3"}}

	d := DeepSimulation{VMSize: "Standard_D4s_v5", SMT: true}
	applySKUCaps(&d, in, ov)
	if !d.Available || !d.CatalogKnown || d.EphemeralOSDisk == nil || *d.EphemeralOSDisk || !strings.Contains(strings.Join(d.Notes, " "), "efêmero") {
		t.Errorf("D4s_v5 em pool efêmero: %+v", d)
	}

	f := DeepSimulation{VMSize: "Standard_F4as_v6", SMT: true}
	applySKUCaps(&f, in, ov)
	if f.SMT || !f.Available || !strings.Contains(strings.Join(f.Notes, " "), "zona(s) 3") {
		t.Errorf("F4as_v6 (sem SMT, zona 3 restrita): %+v", f)
	}

	e := DeepSimulation{VMSize: "Standard_E4as_v5"}
	applySKUCaps(&e, in, ov)
	if e.Available || !strings.Contains(strings.Join(e.Notes, " "), "restrita") {
		t.Errorf("E4as_v5 restrita: %+v", e)
	}

	x := DeepSimulation{VMSize: "Standard_D4as_v6"}
	applySKUCaps(&x, in, ov)
	if x.Available || !strings.Contains(strings.Join(x.Notes, " "), "não oferecida") {
		t.Errorf("SKU fora do catálogo: %+v", x)
	}

	sem := DeepSimulation{VMSize: "Standard_D4s_v5"}
	applySKUCaps(&sem, DeepAnalysisInput{}, ov)
	if !sem.Available || sem.CatalogKnown {
		t.Errorf("sem catálogo deveria assumir disponível: %+v", sem)
	}
}

// A melhor alternativa nunca é uma SKU indisponível na região.
func TestBuildPoolDeepAnalysisIgnoraSKUIndisponivelNaRecomendacao(t *testing.T) {
	in := fixtureCalculofrete(true, true)
	for i := 0; i < 20; i++ { // pool maior: 6 nodes recomendados passariam do limite de impacto
		in.Nodes = append(in.Nodes, in.Nodes[i%10])
		in.Nodes[len(in.Nodes)-1].Name += "-x"
	}
	in.SKUCaps, _ = ParseAzureSKUList([]byte(skuListFixture), "brazilsouth")
	in.Prices["standard_e4as_v5"] = DeepPrice{USDHour: 0.01, Source: "api"} // barata, mas restrita
	a := BuildPoolDeepAnalysis(in)
	if f, ok := findingCodes(a.Findings)["vm_change_savings"]; ok && strings.Contains(f.Detail, "E4as_v5") {
		t.Errorf("recomendou SKU restrita: %s", f.Detail)
	}
	for _, s := range a.Simulation {
		if s.VMSize == "Standard_E4as_v5" && s.Available {
			t.Error("E4as_v5 deveria estar indisponível")
		}
	}
}

func TestParseAzureSKUListFormatoDaAPI(t *testing.T) {
	m, err := ParseAzureSKUList([]byte(`{"value":`+skuListFixture+`}`), "brazilsouth")
	if err != nil || len(m) != 3 {
		t.Fatalf("formato {value: [...]}: %d SKUs, err=%v", len(m), err)
	}
}

func TestSKUCatalogStoreGetWait(t *testing.T) {
	s := NewSKUCatalogStore(t.TempDir())
	s.runner = func(ctx context.Context, region string) ([]byte, error) {
		time.Sleep(50 * time.Millisecond)
		return []byte(skuListFixture), nil
	}
	if c, st, _ := s.GetWait("brazilsouth", 2*time.Second); c == nil || st != SKUCatalogFresh {
		t.Fatalf("GetWait deveria esperar a primeira carga: status=%s", st)
	}

	lento := NewSKUCatalogStore(t.TempDir())
	lento.runner = func(ctx context.Context, region string) ([]byte, error) {
		time.Sleep(500 * time.Millisecond)
		return []byte(skuListFixture), nil
	}
	start := time.Now()
	if c, st, _ := lento.GetWait("brazilsouth", 20*time.Millisecond); c != nil || st != SKUCatalogLoading {
		t.Errorf("com espera curta deveria devolver loading: status=%s", st)
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Error("GetWait não respeitou o tempo máximo de espera")
	}
}
