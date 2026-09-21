package finops

import "testing"

// O AKS/registro às vezes traz a letra da família em minúsculo ("Standard_f4s_v2", 13 pools da frota),
// e a API de preços da Azure diferencia caixa ('F4s v2' acha 2 itens, 'f4s v2' acha 0). A defesa é
// normalizeVMSize, chamada no início de GetPrice/GetVMSpecs — este teste trava isso: sem ela, o pool
// cairia no preço estimado por família e/ou ficaria sem specs.
func TestAzureSKUCaseIsNormalizedBeforePricing(t *testing.T) {
	cases := map[string]string{
		"Standard_f4s_v2":  "Standard_F4s_v2",
		"Standard_F4s_v2":  "Standard_F4s_v2",
		"Standard_d4as_v4": "Standard_D4as_v4",
		"Standard_d2s_v4":  "Standard_D2s_v4",
	}
	for in, want := range cases {
		got := normalizeVMSize(in)
		if got != want {
			t.Errorf("normalizeVMSize(%q) = %q, want %q", in, got, want)
		}
		// É o nome já normalizado que vira o skuName da API de preços (com espaço no lugar de "_").
		if sku := vmSizeToSKUName(got); sku[0] < 'A' || sku[0] > 'Z' {
			t.Errorf("skuName %q enviado à API precisa começar em maiúscula", sku)
		}
	}
	lc, lcMem := GetVMSpecs("Standard_f4s_v2")
	uc, ucMem := GetVMSpecs("Standard_F4s_v2")
	if lc == 0 || lc != uc || lcMem != ucMem {
		t.Errorf("specs devem ser iguais nas duas grafias: %dvCPU/%dGB vs %dvCPU/%dGB", lc, lcMem, uc, ucMem)
	}
}
