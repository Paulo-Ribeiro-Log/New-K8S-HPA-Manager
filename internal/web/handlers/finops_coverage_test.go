package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s-hpa-manager/internal/finops"
	"k8s-hpa-manager/internal/storage"
)

func altsJSON(t *testing.T, alts ...finops.VMAlternative) string {
	t.Helper()
	b, err := json.Marshal(alts)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// O caso real do oferta-prd: calculofrete (F4s_v2) 99% em reserva e a sugestão F4s_v2 → D4s_v5.
func TestNodePoolTierResponses_AvisaReservaNaAlternativa(t *testing.T) {
	raw := []storage.NodePoolTierSuggestion{
		{Cluster: "c", NodePool: "calculofrete", CurrentSKU: "Standard_F4s_v2", NodeCount: 52,
			AlternativesJSON: altsJSON(t, finops.VMAlternative{VMSize: "Standard_D4s_v5", PriceUSDHour: 0.192, CostDeltaPct: -26.7, MonthlySavingsBRL: 13665, Verdict: "recommended"})},
		{Cluster: "c", NodePool: "infratoospot", CurrentSKU: "Standard_D2ads_v6",
			AlternativesJSON: altsJSON(t, finops.VMAlternative{VMSize: "Standard_D2as_v6", Verdict: "consider"})},
		{Cluster: "c", NodePool: "nuncaconsultado", CurrentSKU: "Standard_F4s_v2",
			AlternativesJSON: altsJSON(t, finops.VMAlternative{VMSize: "Standard_D4s_v5", Verdict: "consider"})},
	}
	now := time.Now()
	cov := map[string]*finops.PoolCoverage{
		"calculofrete": {Reservation: 0.99, OnDemand: 0.01, WindowDays: 30, FetchedAt: now, EffectiveCost: 15023, Currency: "BRL"},
		"infratoospot": {Spot: 1, WindowDays: 30, FetchedAt: now},
	}
	rateCalls := 0
	got := nodePoolTierResponses(raw, finops.PerfSet{}, cov, func() float64 { rateCalls++; return 5.1426 }, finops.SKUCoverageIndex{})

	if got[0].Coverage == nil || got[0].Coverage.Reservation < 0.98 {
		t.Fatalf("pool coberto precisa expor a cobertura: %+v", got[0].Coverage)
	}
	w := got[0].Alternatives[0].CoverageWarning
	if w == nil || w.Level != finops.CoverageLevelReservation || !strings.Contains(w.Note, "99%") {
		t.Fatalf("a alternativa do pool em reserva precisa do aviso: %+v", w)
	}
	// Cenários: melhor caso = economia de tabela; pior caso = a reserva ociosa + D4s_v5 sob demanda.
	sc := w.Scenarios
	if sc == nil || sc.BestCaseSavingsBRL != 13665 || sc.WorstCaseSavingsBRL > -37000 || sc.WorstCaseSavingsBRL < -37500 {
		t.Fatalf("esperava melhor caso R$ 13.665 e pior caso ~-R$ 37,3 mil: %+v", sc)
	}
	// O câmbio só é buscado quando há pool com reserva (lazy) e uma única vez por resposta.
	if rateCalls != 1 {
		t.Errorf("câmbio deveria ser consultado 1x (só o pool com reserva), got %d", rateCalls)
	}
	// A economia continua exatamente a que o cálculo de tabela deu (o aviso é informativo).
	if got[0].Alternatives[0].MonthlySavingsBRL != 13665 {
		t.Errorf("o aviso NÃO pode alterar a economia calculada: %v", got[0].Alternatives[0].MonthlySavingsBRL)
	}
	// Pool spot: cobertura exposta, mas sem aviso (reserva não se aplica).
	if got[1].Coverage == nil || got[1].Alternatives[0].CoverageWarning != nil {
		t.Errorf("pool spot: cobertura sim, aviso não: %+v / %+v", got[1].Coverage, got[1].Alternatives[0].CoverageWarning)
	}
	// Pool nunca consultado: nenhum dado inventado.
	if got[2].Coverage != nil || got[2].Alternatives[0].CoverageWarning != nil {
		t.Errorf("sem consulta, nada de cobertura/aviso: %+v", got[2])
	}
}

func TestNodePoolTierResponses_ChavesDeCoberturaEmMinusculas(t *testing.T) {
	raw := []storage.NodePoolTierSuggestion{{Cluster: "c", NodePool: "CalculoFrete", CurrentSKU: "Standard_F4s_v2",
		AlternativesJSON: altsJSON(t, finops.VMAlternative{VMSize: "Standard_F2s_v2"})}}
	got := nodePoolTierResponses(raw, finops.PerfSet{}, map[string]*finops.PoolCoverage{"calculofrete": {Reservation: 0.9}}, nil, finops.SKUCoverageIndex{})
	if got[0].Coverage == nil {
		t.Error("o nome do pool deve casar sem depender da caixa (o Cost Management devolve minúsculas)")
	}
}

func TestNodePoolTierResponses_SemPoolComReservaNaoBuscaCambio(t *testing.T) {
	raw := []storage.NodePoolTierSuggestion{{Cluster: "c", NodePool: "spotpool", CurrentSKU: "Standard_D2ads_v6",
		AlternativesJSON: altsJSON(t, finops.VMAlternative{VMSize: "Standard_D2as_v6"})}}
	calls := 0
	nodePoolTierResponses(raw, finops.PerfSet{}, map[string]*finops.PoolCoverage{"spotpool": {Spot: 1}}, func() float64 { calls++; return 5 }, finops.SKUCoverageIndex{})
	if calls != 0 {
		t.Errorf("sem pool com reserva a leitura não pode nem tentar buscar o câmbio (pode ir à rede), got %d chamadas", calls)
	}
}

// A etiqueta "reserva na frota" vem do índice na LEITURA (uma atualização do índice aparece sem
// reanalisar), e só nas alternativas cujo SKU (ou série) tem cobertura conhecida.
func TestNodePoolTierResponses_EtiquetaReservaDaFrota(t *testing.T) {
	ix := finops.BuildSKUCoverageIndex([]storage.SKUPricingCoverage{
		{SubscriptionID: "s", SKU: "Standard_D4s_v4", Model: "reservation", Cost: 31926, WindowDays: 30},
		{SubscriptionID: "s", SKU: "Standard_F4s_v2", Model: "reservation", Cost: 52186, WindowDays: 30},
	})
	raw := []storage.NodePoolTierSuggestion{{Cluster: "c", NodePool: "calculofrete", CurrentSKU: "Standard_F4s_v2",
		AlternativesJSON: altsJSON(t,
			finops.VMAlternative{VMSize: "Standard_D4s_v4"},
			finops.VMAlternative{VMSize: "Standard_D4s_v5"},
			finops.VMAlternative{VMSize: "Standard_F2s_v2"})}}
	got := nodePoolTierResponses(raw, finops.PerfSet{}, nil, nil, ix)[0].Alternatives

	if h := got[0].ReservedHint; h == nil || h.Kind != finops.HintKindReservation || h.Scope != finops.HintScopeSKU {
		t.Errorf("D4s_v4 roda em reserva: %+v", h)
	}
	if got[1].ReservedHint != nil {
		t.Errorf("D4s_v5 não tem cobertura conhecida: %+v", got[1].ReservedHint)
	}
	if h := got[2].ReservedHint; h == nil || h.Scope != finops.HintScopeSeries {
		t.Errorf("F2s_v2 é da série reservada da F4s_v2: %+v", h)
	}
}

// O refresh automático roda dentro do scan e NUNCA pode derrubá-lo: sem store (ou sem kube manager) só
// retorna, sem panic e sem tentar a rede.
func TestAutoRefreshCoverage_SemStoreNaoFazNada(t *testing.T) {
	h := &FinOpsHandler{}
	h.autoRefreshCoverage(context.Background(), "qualquer-cluster") // não pode dar panic
	if idx := h.skuCoverageIndex(); !idx.Empty() {
		t.Error("sem store o índice é vazio")
	}
	if got := h.poolCoverage("c"); len(got) != 0 {
		t.Errorf("sem store a cobertura é vazia: %+v", got)
	}
}
