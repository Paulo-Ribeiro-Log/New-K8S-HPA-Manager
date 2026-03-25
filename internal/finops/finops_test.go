package finops

import (
	"fmt"
	"testing"
)

func TestVMSizeToSKUName(t *testing.T) {
	cases := map[string]string{
		"Standard_D4s_v3":  "D4s v3",
		"Standard_E8s_v5":  "E8s v5",
		"Standard_B4ms":    "B4ms",
		"Standard_F8s_v2":  "F8s v2",
		"Standard_D16s_v3": "D16s v3",
	}
	for input, want := range cases {
		got := vmSizeToSKUName(input)
		if got != want {
			t.Errorf("vmSizeToSKUName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGetVMSpecs(t *testing.T) {
	cpu, mem := GetVMSpecs("Standard_D4s_v3")
	if cpu != 4 || mem != 16 {
		t.Errorf("Standard_D4s_v3: got cpu=%d mem=%d, want 4/16", cpu, mem)
	}
	cpu, mem = GetVMSpecs("Standard_E8s_v3")
	if cpu != 8 || mem != 64 {
		t.Errorf("Standard_E8s_v3: got cpu=%d mem=%d, want 8/64", cpu, mem)
	}
}

func TestInferSpecsFromName(t *testing.T) {
	cpu, mem := inferSpecsFromName("Standard_D8s_v99") // não está na tabela
	if cpu != 8 {
		t.Errorf("inferir D8: got cpu=%d, want 8", cpu)
	}
	_ = mem
}

func TestFallbackPrices(t *testing.T) {
	skus := []string{"Standard_D4s_v3", "Standard_E8s_v3", "Standard_F4s_v2", "Standard_B2s"}
	for _, sku := range skus {
		price, ok := fallbackPrices[sku]
		if !ok || price <= 0 {
			t.Errorf("fallback price para %s não encontrado ou inválido: %v", sku, price)
		}
		monthly := price * HoursPerMonth
		fmt.Printf("  %-25s  $%.3f/h  $%.1f/mês\n", sku, price, monthly)
	}
}

func TestExchangeRateFallback(t *testing.T) {
	er := NewExchangeRateProvider()
	rate, date := er.Get()
	if rate <= 0 {
		t.Errorf("cotação inválida: %v", rate)
	}
	if date == "" {
		t.Error("data da cotação vazia")
	}
	t.Logf("USD/BRL: %.4f  data: %s", rate, date)
}

func TestCostExample(t *testing.T) {
	// Simula custo de 5 nodes Standard_D4s_v3 por 1 mês
	price := fallbackPrices["Standard_D4s_v3"]
	er := NewExchangeRateProvider()
	rate, _ := er.Get()

	costUSD := price * HoursPerMonth * 5
	costBRL := costUSD * rate
	t.Logf("5x Standard_D4s_v3: $%.2f USD = R$ %.2f BRL/mês", costUSD, costBRL)
	if costUSD <= 0 || costBRL <= 0 {
		t.Error("custo calculado inválido")
	}
}
