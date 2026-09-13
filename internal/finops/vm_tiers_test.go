package finops

import (
	"fmt"
	"testing"
)

// fakePricer implementa CloudPricer com specs/preços fixos — evita qualquer chamada de rede
// real (Azure Retail API/GCP Billing/AWS Pricing) nos testes.
type fakePricer struct {
	specs  map[string][2]int // sku -> [vCPUs, memGB]
	prices map[string]float64
}

func (f *fakePricer) GetVMSpecs(sku string) (int, int) {
	if s, ok := f.specs[sku]; ok {
		return s[0], s[1]
	}
	return 0, 0
}

func (f *fakePricer) GetPrice(sku string) (float64, string, error) {
	if p, ok := f.prices[sku]; ok {
		return p, "fake", nil
	}
	return 0, "", fmt.Errorf("sku desconhecido no fakePricer: %s", sku)
}

func TestSuggestVMTier_Azure_MemBottleneckSwitchesToEFamily(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"Standard_D4s_v5": {4, 16},
			"Standard_E4s_v5": {4, 32},
		},
		prices: map[string]float64{
			"Standard_D4s_v5": 0.20,
			"Standard_E4s_v5": 0.30,
		},
	}
	// D4s_v5 = 4 GB/vCPU → gargalo de memória (mem alto, cpu baixo) deve sugerir E-series
	// (8 GB/vCPU) com os mesmos 4 vCPUs.
	alts := SuggestVMTier("aks", "Standard_D4s_v5", 20, 80, pricer, 5.0, 3)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "Standard_E4s_v5" {
		t.Errorf("VMSize = %q, want %q", alts[0].VMSize, "Standard_E4s_v5")
	}
	if alts[0].Verdict != "recommended" {
		t.Errorf("Verdict = %q, want %q", alts[0].Verdict, "recommended")
	}
	if alts[0].MonthlySavingsBRL >= 0 {
		t.Errorf("MonthlySavingsBRL = %v, esperava negativo (E-series é mais cara que D-series)", alts[0].MonthlySavingsBRL)
	}
}

func TestSuggestVMTier_Azure_OversizedSuggestsHalfSize(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"Standard_D8s_v5": {8, 32},
			"Standard_D4s_v5": {4, 16},
		},
		prices: map[string]float64{
			"Standard_D8s_v5": 0.40,
			"Standard_D4s_v5": 0.20,
		},
	}
	alts := SuggestVMTier("aks", "Standard_D8s_v5", 10, 10, pricer, 5.0, 2)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "Standard_D4s_v5" {
		t.Errorf("VMSize = %q, want %q", alts[0].VMSize, "Standard_D4s_v5")
	}
	if alts[0].Verdict != "cheaper" {
		t.Errorf("Verdict = %q, want %q", alts[0].Verdict, "cheaper")
	}
	if alts[0].MonthlySavingsBRL <= 0 {
		t.Errorf("MonthlySavingsBRL = %v, esperava positivo (metade do tamanho é mais barato)", alts[0].MonthlySavingsBRL)
	}
}

func TestSuggestVMTier_GKE_CPUHeavySwitchesTier(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"e2-standard-4": {4, 16},
			"e2-highcpu-4":  {4, 4},
		},
		prices: map[string]float64{
			"e2-standard-4": 0.15,
			"e2-highcpu-4":  0.10,
		},
	}
	alts := SuggestVMTier("gke", "e2-standard-4", 80, 15, pricer, 5.0, 3)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "e2-highcpu-4" {
		t.Errorf("VMSize = %q, want %q", alts[0].VMSize, "e2-highcpu-4")
	}
}

func TestSuggestVMTier_GKE_MemBottleneckSwitchesToHighmem(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"n2-standard-4": {4, 16},
			"n2-highmem-4":  {4, 32},
		},
		prices: map[string]float64{
			"n2-standard-4": 0.20,
			"n2-highmem-4":  0.28,
		},
	}
	alts := SuggestVMTier("gke", "n2-standard-4", 15, 80, pricer, 5.0, 2)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "n2-highmem-4" {
		t.Errorf("VMSize = %q, want %q", alts[0].VMSize, "n2-highmem-4")
	}
}

func TestSuggestVMTier_EKS_MemBottleneckSwitchesToMemoryFamily(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"m5.xlarge":  {4, 16},
			"r6i.xlarge": {4, 32},
			"r5.xlarge":  {4, 32},
		},
		prices: map[string]float64{
			"m5.xlarge":  0.192,
			"r6i.xlarge": 0.252,
			"r5.xlarge":  0.252,
		},
	}
	alts := SuggestVMTier("eks", "m5.xlarge", 15, 80, pricer, 5.0, 2)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "r6i.xlarge" {
		t.Errorf("VMSize = %q, want %q (primeira família do grupo 'memory')", alts[0].VMSize, "r6i.xlarge")
	}
}

func TestSuggestVMTier_EKS_OversizedGoesDownOneSize(t *testing.T) {
	pricer := &fakePricer{
		specs: map[string][2]int{
			"m5.xlarge": {4, 16},
			"m5.large":  {2, 8},
		},
		prices: map[string]float64{
			"m5.xlarge": 0.192,
			"m5.large":  0.096,
		},
	}
	alts := SuggestVMTier("eks", "m5.xlarge", 10, 10, pricer, 5.0, 2)
	if len(alts) == 0 {
		t.Fatal("esperava ao menos 1 alternativa, veio vazio")
	}
	if alts[0].VMSize != "m5.large" {
		t.Errorf("VMSize = %q, want %q", alts[0].VMSize, "m5.large")
	}
}

func TestSuggestVMTier_NoUsageDataReturnsNoCandidates(t *testing.T) {
	pricer := &fakePricer{
		specs:  map[string][2]int{"Standard_D4s_v5": {4, 16}},
		prices: map[string]float64{"Standard_D4s_v5": 0.20},
	}
	// cpuUtilPct=0 e memUtilPct=0 → hasUsage=false → nenhuma sugestão de troca de família.
	alts := SuggestVMTier("aks", "Standard_D4s_v5", 0, 0, pricer, 5.0, 1)
	if len(alts) != 0 {
		t.Errorf("esperava 0 alternativas sem dado de uso, veio %d", len(alts))
	}
}

func TestSuggestVMTier_UnknownCurrentSKUReturnsNil(t *testing.T) {
	pricer := &fakePricer{specs: map[string][2]int{}, prices: map[string]float64{}}
	alts := SuggestVMTier("aks", "Standard_Unknown_v9", 80, 80, pricer, 5.0, 1)
	if alts != nil {
		t.Errorf("esperava nil pra SKU desconhecido, veio %v", alts)
	}
}
