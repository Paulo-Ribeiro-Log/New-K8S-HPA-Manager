package finops

import (
	"reflect"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/storage"
)

func covRow(sku, model string, cost float64) storage.SKUPricingCoverage {
	return storage.SKUPricingCoverage{SubscriptionID: "s", SKU: sku, Model: model, Cost: cost, WindowDays: 30}
}

// Coberturas REAIS observadas no Cost Management da subscription (custo amortizado, 30 dias).
func realIndex() SKUCoverageIndex {
	return BuildSKUCoverageIndex([]storage.SKUPricingCoverage{
		covRow("Standard_F4s_v2", "reservation", 52186),
		covRow("Standard_D4s_v4", "reservation", 31926),
		covRow("Standard_D8s_v5", "savingsplan", 8973),
		covRow("Standard_D2s_v4", "reservation", 6661),
	})
}

func TestSKUCoverageIndex_Hint(t *testing.T) {
	ix := realIndex()

	h := ix.Hint("Standard_D4s_v4")
	if h == nil || h.Kind != HintKindReservation || h.Scope != HintScopeSKU || !strings.Contains(h.Note, "capacidade ociosa") {
		t.Fatalf("D4s_v4 roda em reserva (SKU exato) e a nota deve avisar que a ociosidade é desconhecida: %+v", h)
	}
	if h := ix.Hint("standard_f4s_V2"); h == nil || h.Scope != HintScopeSKU {
		t.Errorf("a caixa do SKU não pode importar: %+v", h)
	}
	if h := ix.Hint("Standard_D8s_v5"); h == nil || h.Kind != HintKindSavingsPlan || h.Scope != HintScopeSKU {
		t.Errorf("D8s_v5 roda em Savings Plan: %+v", h)
	}
	// Outro tamanho da mesma série reservada (F4s_v2 → F2s_v2): vale com flexibilidade.
	if h := ix.Hint("Standard_F2s_v2"); h == nil || h.Kind != HintKindReservation || h.Scope != HintScopeSeries {
		t.Errorf("F2s_v2 é da série reservada da F4s_v2: %+v", h)
	}
	// Savings Plan NÃO se propaga por série (cobre várias famílias: nada a dizer sobre outro tamanho).
	if h := ix.Hint("Standard_D4s_v5"); h != nil {
		t.Errorf("D4s_v5: só o D8s_v5 tem plano, e plano não vale por série: %+v", h)
	}
	if h := ix.Hint("Standard_E4s_v4"); h != nil {
		t.Errorf("SKU sem nenhuma cobertura → nil: %+v", h)
	}
}

func TestSKUCoverageIndex_IgnoraRuidoEModelosNaoCobertos(t *testing.T) {
	ix := BuildSKUCoverageIndex([]storage.SKUPricingCoverage{
		covRow("Standard_D2s_v5", "reservation", 10), // ~R$ 10/mês: ruído
		covRow("Standard_D2s_v4", "ondemand", 99999), // sob demanda não é cobertura
	})
	if !ix.Empty() {
		t.Error("índice só com ruído/sob demanda deveria ficar vazio")
	}
	var zero SKUCoverageIndex
	if !zero.Empty() || zero.Hint("Standard_F4s_v2") != nil {
		t.Error("índice zero (nunca consultado) não pode dar panic nem inventar cobertura")
	}
}

func TestSKUCoverageIndex_NormalizaJanelaParaMes(t *testing.T) {
	// 60 dias: o custo da janela vale metade por mês.
	ix := BuildSKUCoverageIndex([]storage.SKUPricingCoverage{{SKU: "Standard_F4s_v2", Model: "reservation", Cost: 12000, WindowDays: 60}})
	h := ix.Hint("Standard_F4s_v2")
	if h == nil || !strings.Contains(h.Note, "R$ 6.083") { // 12000 * (730/24)/60
		t.Errorf("custo mensal esperado ~R$ 6.083: %+v", h)
	}
}

func TestAzureGenerationTwins(t *testing.T) {
	if got, want := azureGenerationTwins("Standard_D4s_v5"), []string{"Standard_D4s_v3", "Standard_D4s_v4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("gêmeos de D4s_v5 = %v, want %v", got, want)
	}
	if got, want := azureGenerationTwins("standard_d2s_v4"), []string{"Standard_D2s_v3", "Standard_D2s_v5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("gêmeos de d2s_v4 = %v, want %v", got, want)
	}
	// Só uma geração na tabela, tamanho desconhecido e SKU fora do padrão: nada.
	for _, sku := range []string{"Standard_F4s_v2", "Standard_D3s_v5", "n1-standard-4", "Standard_B2s", ""} {
		if got := azureGenerationTwins(sku); len(got) != 0 {
			t.Errorf("azureGenerationTwins(%q) deveria ser vazio, got %v", sku, got)
		}
	}
}

func twinPricer() *fakePricer {
	return &fakePricer{
		specs: map[string][2]int{
			"Standard_F4s_v2": {4, 8}, "Standard_D4s_v5": {4, 16}, "Standard_D4s_v4": {4, 16}, "Standard_D4s_v3": {4, 16},
		},
		prices: map[string]float64{
			"Standard_F4s_v2": 0.262, "Standard_D4s_v5": 0.192, "Standard_D4s_v4": 0.204, "Standard_D4s_v3": 0.215,
		},
	}
}

// O caso do calculofrete: F4s_v2 com gargalo de memória → SuggestVMTier oferece D4s_v5. A frota tem
// D4s_v4 reservada, então a v4 deve ser oferecida também, na frente da v5 sem cobertura.
func TestAugmentWithCoveredTwins_OfereceGemeoReservado(t *testing.T) {
	pricer := twinPricer()
	base := SuggestVMTier("aks", "Standard_F4s_v2", 41, 99, pricer, 5.1426, 52)
	if len(base) != 1 || base[0].VMSize != "Standard_D4s_v5" {
		t.Fatalf("pré-condição: SuggestVMTier deveria oferecer só a D4s_v5: %+v", base)
	}

	got := AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, realIndex())
	if len(got) != 2 {
		t.Fatalf("esperado D4s_v5 + o gêmeo reservado D4s_v4: %+v", got)
	}
	if got[0].VMSize != "Standard_D4s_v4" || got[1].VMSize != "Standard_D4s_v5" {
		t.Errorf("a alternativa com reserva vem primeiro: %s, %s", got[0].VMSize, got[1].VMSize)
	}
	v4 := got[0]
	if v4.Verdict != base[0].Verdict {
		t.Errorf("o gêmeo herda o veredito do irmão: %q vs %q", v4.Verdict, base[0].Verdict)
	}
	if !strings.Contains(v4.Reason, "geração anterior") || !strings.Contains(v4.Reason, "Standard_D4s_v4") || !strings.Contains(v4.Reason, "reserva") {
		t.Errorf("a razão deve explicar de onde veio: %s", v4.Reason)
	}
	// Preço e economia continuam a preço de tabela, calculados igual às demais.
	wantSav := (0.262 - 0.204) * 730 * 52 * 5.1426
	if v4.PriceUSDHour != 0.204 || v4.MonthlySavingsBRL < wantSav-1 || v4.MonthlySavingsBRL > wantSav+1 {
		t.Errorf("preço/economia de tabela do gêmeo: %v / %v (esperado ~%.0f)", v4.PriceUSDHour, v4.MonthlySavingsBRL, wantSav)
	}
	// D4s_v3 não roda coberta: não entra.
	for _, a := range got {
		if a.VMSize == "Standard_D4s_v3" {
			t.Error("D4s_v3 não tem cobertura conhecida — não pode ser oferecida como 'gêmeo coberto'")
		}
	}
}

func TestAugmentWithCoveredTwins_PlanoVemDepoisDaReserva(t *testing.T) {
	ix := BuildSKUCoverageIndex([]storage.SKUPricingCoverage{
		covRow("Standard_D4s_v4", "savingsplan", 5000),
		covRow("Standard_D4s_v3", "reservation", 5000),
	})
	pricer := twinPricer()
	base := SuggestVMTier("aks", "Standard_F4s_v2", 41, 99, pricer, 5.1426, 52)
	got := AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, ix)
	var order []string
	for _, a := range got {
		order = append(order, a.VMSize)
	}
	want := []string{"Standard_D4s_v3", "Standard_D4s_v4", "Standard_D4s_v5"} // reserva, plano, sem cobertura
	if !reflect.DeepEqual(order, want) {
		t.Errorf("ordem = %v, want %v", order, want)
	}
}

func TestAugmentWithCoveredTwins_NaoMexeQuandoNaoDeve(t *testing.T) {
	pricer := twinPricer()
	base := SuggestVMTier("aks", "Standard_F4s_v2", 41, 99, pricer, 5.1426, 52)
	same := func(name string, got []VMAlternative) {
		if !reflect.DeepEqual(got, base) {
			t.Errorf("%s: as alternativas não podiam mudar: %+v", name, got)
		}
	}
	same("índice vazio", AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, SKUCoverageIndex{}))
	same("não é AKS", AugmentWithCoveredTwins("eks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, realIndex()))
	same("sem pricer", AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, nil, 5.1426, 52, realIndex()))
	// Só a SÉRIE tem cobertura (nenhum tamanho exato do gêmeo): não oferece.
	onlySeries := BuildSKUCoverageIndex([]storage.SKUPricingCoverage{covRow("Standard_D8s_v4", "reservation", 9000)})
	same("cobertura só de outro tamanho da série", AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, onlySeries))
	if got := AugmentWithCoveredTwins("aks", "Standard_F4s_v2", nil, 41, 99, pricer, 5.1426, 52, realIndex()); got != nil {
		t.Errorf("sem alternativas não há o que aumentar: %+v", got)
	}
}

func TestAugmentWithCoveredTwins_MesmaCoberturaPrefereGeracaoNova(t *testing.T) {
	ix := BuildSKUCoverageIndex([]storage.SKUPricingCoverage{
		covRow("Standard_D4s_v4", "reservation", 5000),
		covRow("Standard_D4s_v3", "reservation", 5000),
	})
	pricer := twinPricer()
	base := SuggestVMTier("aks", "Standard_F4s_v2", 41, 99, pricer, 5.1426, 52)
	got := AugmentWithCoveredTwins("aks", "Standard_F4s_v2", base, 41, 99, pricer, 5.1426, 52, ix)
	var order []string
	for _, a := range got {
		order = append(order, a.VMSize)
	}
	want := []string{"Standard_D4s_v4", "Standard_D4s_v3", "Standard_D4s_v5"} // as duas em reserva (v4 antes da v3), depois a sem cobertura
	if !reflect.DeepEqual(order, want) {
		t.Errorf("ordem = %v, want %v", order, want)
	}
}

func TestAzureGeneration(t *testing.T) {
	for sku, want := range map[string]int{"Standard_D4s_v5": 5, "standard_d4s_v10": 10, "Standard_F4s_v2": 2, "Standard_B2s": 0, "": 0} {
		if got := azureGeneration(sku); got != want {
			t.Errorf("azureGeneration(%q) = %d, want %d", sku, got, want)
		}
	}
}
