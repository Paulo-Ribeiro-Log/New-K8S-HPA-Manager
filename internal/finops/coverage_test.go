package finops

import (
	"math"
	"strings"
	"testing"
	"time"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// Números reais do oferta-prd (custo amortizado, 30 dias): calculofrete 99% reserva.
func TestBuildPoolCoverage_CalculoFrete(t *testing.T) {
	cov := BuildPoolCoverage(map[string]float64{
		azure.ModelReservation: 15400, azure.ModelOnDemand: 141,
	}, "BRL", 30, time.Now())
	if cov == nil || math.Abs(cov.Reservation-15400.0/15541.0) > 1e-9 || cov.Reservation < 0.99 {
		t.Fatalf("reserva deveria ser ~99%%: %+v", cov)
	}
	if math.Abs(cov.EffectiveCost-15541) > 1e-9 || cov.Currency != "BRL" || cov.WindowDays != 30 {
		t.Errorf("custo efetivo/moeda/janela errados: %+v", cov)
	}
	if s := cov.Reservation + cov.SavingsPlan + cov.OnDemand + cov.Spot + cov.Other; math.Abs(s-1) > 1e-9 {
		t.Errorf("as participações precisam somar 1, got %v", s)
	}
}

func TestBuildPoolCoverage_SemCustoNaoAfirmaNada(t *testing.T) {
	if BuildPoolCoverage(map[string]float64{}, "BRL", 30, time.Now()) != nil {
		t.Error("pool sem custo na janela → nil (nunca 'cobertura 0%')")
	}
	if BuildPoolCoverage(map[string]float64{azure.ModelReservation: 0, azure.ModelOnDemand: -5}, "BRL", 30, time.Now()) != nil {
		t.Error("custo zero/negativo (estorno) → nil")
	}
}

func TestAssessCoverageWarning_OutraSerieDeixaReservaOciosa(t *testing.T) {
	cov := &PoolCoverage{Reservation: 0.99, OnDemand: 0.01}
	w := AssessCoverageWarning("Standard_F4s_v2", "Standard_D4s_v5", cov)
	if w == nil || w.Level != CoverageLevelReservation {
		t.Fatalf("esperado aviso de reserva: %+v", w)
	}
	for _, want := range []string{"99%", "não é da mesma série", "sob demanda", "absorverem", "preço de tabela", "pode não se realizar"} {
		if !strings.Contains(w.Note, want) {
			t.Errorf("a nota deveria conter %q: %s", want, w.Note)
		}
	}
}

func TestAssessCoverageWarning_MesmaSerie(t *testing.T) {
	w := AssessCoverageWarning("Standard_F4s_v2", "Standard_F2s_v2", &PoolCoverage{Reservation: 0.99})
	if w == nil || !strings.Contains(w.Note, "mesma série") || !strings.Contains(w.Note, "flexibilidade de tamanho") || !strings.Contains(w.Note, "absorverem") {
		t.Fatalf("mesma série: aviso de flexibilidade/consumo proporcional: %+v", w)
	}
	if strings.Contains(w.Note, "não é da mesma série") {
		t.Errorf("não pode dizer que é de outra série: %s", w.Note)
	}
	// Caixa do SKU não pode quebrar o reconhecimento da série (o registro tem 'Standard_f4s_v2').
	if w2 := AssessCoverageWarning("Standard_f4s_v2", "Standard_F2s_v2", &PoolCoverage{Reservation: 0.99}); w2 == nil || !strings.Contains(w2.Note, "mesma série") {
		t.Errorf("SKU em minúsculo deveria ser da mesma série: %+v", w2)
	}
}

func TestAssessCoverageWarning_SavingsPlan(t *testing.T) {
	w := AssessCoverageWarning("Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{SavingsPlan: 0.9, OnDemand: 0.1})
	if w == nil || w.Level != CoverageLevelSavingsPlan || !strings.Contains(w.Note, "várias famílias") || !strings.Contains(w.Note, "comprometido") {
		t.Fatalf("Savings Plan: mantém o desconto mas o compromisso segue cobrado: %+v", w)
	}
}

func TestAssessCoverageWarning_ReservaEPlanoJuntos(t *testing.T) {
	w := AssessCoverageWarning("Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Reservation: 0.6, SavingsPlan: 0.3, OnDemand: 0.1})
	if w == nil || w.Level != CoverageLevelReservation || !strings.Contains(w.Note, "Savings Plan cobre outros ~30%") {
		t.Fatalf("reserva prevalece e cita o plano: %+v", w)
	}
}

func TestAssessCoverageWarning_SemAviso(t *testing.T) {
	cases := map[string]struct {
		cur, alt string
		cov      *PoolCoverage
	}{
		"cobertura não consultada":          {"Standard_F4s_v2", "Standard_D4s_v5", nil},
		"mesmo SKU":                         {"Standard_F4s_v2", "standard_F4S_v2", &PoolCoverage{Reservation: 0.99}},
		"pool spot (reserva não se aplica)": {"Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Spot: 1}},
		"cobertura baixa":                   {"Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Reservation: 0.10, OnDemand: 0.90}},
	}
	for name, c := range cases {
		if w := AssessCoverageWarning(c.cur, c.alt, c.cov); w != nil {
			t.Errorf("%s: não deveria avisar, got %+v", name, w)
		}
	}
}

// Números reais do oferta-prd: calculofrete, 52 nodes F4s_v2, alternativa D4s_v5 (US$ 0,192/h),
// câmbio 5,1426, custo efetivo 30d de R$ 15.023 com 99% em reserva e 1% sob demanda.
func calculoFreteCov() *PoolCoverage {
	return &PoolCoverage{Reservation: 0.99, OnDemand: 0.01, EffectiveCost: 15023, Currency: "BRL", WindowDays: 30}
}

func TestAssessSwapScenarios_OutraSerie_CalculoFrete(t *testing.T) {
	in := SwapInputs{NodeCount: 52, AltPriceUSDHour: 0.192, ExchangeRate: 5.1426, TableSavingsBRL: 13665, CostDeltaPct: -26.7}
	sc := AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", calculoFreteCov(), in)
	if sc == nil {
		t.Fatal("esperava cenários")
	}
	if sc.BestCaseSavingsBRL != 13665 {
		t.Errorf("melhor caso deve ser a economia de tabela, got %v", sc.BestCaseSavingsBRL)
	}
	altAll := 0.192 * 730 * 52 * 5.1426 // 37.481
	odToday := 0.01 * 15023 * (730.0 / 24 / 30)
	want := -(altAll - odToday)
	if math.Abs(sc.WorstCaseSavingsBRL-want) > 0.01 || sc.WorstCaseSavingsBRL > -37000 || sc.WorstCaseSavingsBRL < -37500 {
		t.Errorf("pior caso deveria ser ~-R$ 37,3 mil (a reserva segue paga + D4s_v5 sob demanda): got %.0f, want %.0f", sc.WorstCaseSavingsBRL, want)
	}
	for _, s := range []string{"R$ 37.481", "ociosa", "52 nodes", "R$ 13.665", "escopo dela"} {
		if !strings.Contains(sc.Note, s) {
			t.Errorf("a nota deveria conter %q: %s", s, sc.Note)
		}
	}
}

func TestAssessSwapScenarios_MesmaSerieMenor(t *testing.T) {
	in := SwapInputs{NodeCount: 52, AltPriceUSDHour: 0.131, ExchangeRate: 5.1426, TableSavingsBRL: 984, CostDeltaPct: -50}
	sc := AssessSwapScenarios("Standard_F4s_v2", "Standard_F2s_v2", calculoFreteCov(), in)
	if sc == nil {
		t.Fatal("esperava cenários")
	}
	odToday := 0.01 * 15023 * (730.0 / 24 / 30)
	if math.Abs(sc.WorstCaseSavingsBRL-odToday*0.5) > 0.01 {
		t.Errorf("pior caso = só a fatia sob demanda economiza 50%%: got %.2f want %.2f", sc.WorstCaseSavingsBRL, odToday*0.5)
	}
	if sc.WorstCaseSavingsBRL < 0 || sc.WorstCaseSavingsBRL > 100 {
		t.Errorf("na mesma série a reserva não encolhe: economia efetiva ≈ R$ 0-100, got %v", sc.WorstCaseSavingsBRL)
	}
	if !strings.Contains(sc.Note, "não encolhe") {
		t.Errorf("a nota deve explicar por quê: %s", sc.Note)
	}
}

func TestAssessSwapScenarios_SemChute(t *testing.T) {
	ok := SwapInputs{NodeCount: 52, AltPriceUSDHour: 0.192, ExchangeRate: 5.1426, TableSavingsBRL: 13665, CostDeltaPct: -26.7}
	cases := map[string]*SwapScenarios{
		"sem cobertura":                  AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", nil, ok),
		"mesmo SKU":                      AssessSwapScenarios("Standard_F4s_v2", "standard_f4s_V2", calculoFreteCov(), ok),
		"reserva baixa":                  AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Reservation: 0.1, OnDemand: 0.9, EffectiveCost: 1000, Currency: "BRL", WindowDays: 30}, ok),
		"savings plan relevante":         AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Reservation: 0.5, SavingsPlan: 0.4, EffectiveCost: 1000, Currency: "BRL", WindowDays: 30}, ok),
		"moeda não BRL":                  AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", &PoolCoverage{Reservation: 0.99, EffectiveCost: 1000, Currency: "USD", WindowDays: 30}, ok),
		"sem nodes":                      AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", calculoFreteCov(), SwapInputs{AltPriceUSDHour: 0.19, ExchangeRate: 5}),
		"sem câmbio":                     AssessSwapScenarios("Standard_F4s_v2", "Standard_D4s_v5", calculoFreteCov(), SwapInputs{NodeCount: 5, AltPriceUSDHour: 0.19}),
		"mesma série para tamanho maior": AssessSwapScenarios("Standard_F4s_v2", "Standard_F8s_v2", calculoFreteCov(), SwapInputs{NodeCount: 5, AltPriceUSDHour: 0.5, ExchangeRate: 5, CostDeltaPct: 100}),
	}
	for name, sc := range cases {
		if sc != nil {
			t.Errorf("%s: não deveria calcular cenários (sem chute), got %+v", name, sc)
		}
	}
}

func TestBrlInt(t *testing.T) {
	cases := map[float64]string{0: "R$ 0", 999: "R$ 999", 1000: "R$ 1.000", 37481.6: "R$ 37.482", 1234567: "R$ 1.234.567", -37329.4: "-R$ 37.329"}
	for in, want := range cases {
		if got := brlInt(in); got != want {
			t.Errorf("brlInt(%v) = %q, want %q", in, got, want)
		}
	}
}
