package azure

import (
	"reflect"
	"testing"
)

func TestSKUsFromMeter(t *testing.T) {
	cases := map[string][]string{
		// Medidores REAIS lidos do Cost Management da subscription.
		"F4s v2":              {"Standard_F4s_v2"},
		"D4s v4":              {"Standard_D4s_v4"},
		"D8ls v5":             {"Standard_D8ls_v5"},
		"E4s v4":              {"Standard_E4s_v4"},
		"D8a v4/D8as v4":      {"Standard_D8a_v4", "Standard_D8as_v4"},
		"D4a v4/D4as v4":      {"Standard_D4a_v4", "Standard_D4as_v4"},
		"D8 v3/D8s v3":        {"Standard_D8_v3", "Standard_D8s_v3"},
		"F16s v2":             {"Standard_F16s_v2"},
		"DC4s v3":             {"Standard_DC4s_v3"},
		"  D2s v4 ":           {"Standard_D2s_v4"},
		"D4s v4 Windows":      nil, // medidor de Windows: não é o SKU puro
		"F4s v2 Low Priority": nil,
		"Cache Reads":         nil,
		"":                    nil,
	}
	for in, want := range cases {
		if got := SKUsFromMeter(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SKUsFromMeter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAccumulateSKUCost(t *testing.T) {
	body := []byte(`{"properties":{
	  "columns":[{"name":"Cost"},{"name":"PricingModel"},{"name":"Meter"},{"name":"Currency"}],
	  "rows":[
	    [52186.4,"Reservation","F4s v2","BRL"],
	    [31926,"Reservation","D4s v4","BRL"],
	    [8973,"SavingsPlan","D8s v5","BRL"],
	    [5643,"SavingsPlan","D8a v4/D8as v4","BRL"],
	    [100,"OnDemand","D2s v5","BRL"],
	    [50,"Reservation","F4s v2","BRL"],
	    [77,"Reservation","Algum Medidor","BRL"]
	  ]}}`)
	var out SKUCost
	out.BySKU = map[string]map[string]float64{}
	if _, err := accumulateSKUCost(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Currency != "BRL" {
		t.Errorf("moeda = %q", out.Currency)
	}
	if got := out.BySKU["Standard_F4s_v2"][ModelReservation]; got != 52236.4 {
		t.Errorf("F4s_v2 deveria somar as duas linhas: %v", got)
	}
	if out.BySKU["Standard_D8s_v5"][ModelSavingsPlan] != 8973 {
		t.Errorf("D8s_v5 no Savings Plan: %+v", out.BySKU["Standard_D8s_v5"])
	}
	// Medidor com 2 tamanhos vira os dois SKUs.
	if out.BySKU["Standard_D8a_v4"][ModelSavingsPlan] != 5643 || out.BySKU["Standard_D8as_v4"][ModelSavingsPlan] != 5643 {
		t.Errorf("medidor 'D8a v4/D8as v4' deveria marcar os dois: %+v", out.BySKU)
	}
	if _, has := out.BySKU["Standard_D2s_v5"]; has {
		t.Error("sob demanda não entra no índice de cobertura")
	}
	if len(out.BySKU) != 5 {
		t.Errorf("medidor fora do padrão não pode virar SKU: %v", out.BySKU)
	}
}

func TestAccumulateSKUCost_SemColunaEhErro(t *testing.T) {
	var out SKUCost
	out.BySKU = map[string]map[string]float64{}
	if _, err := accumulateSKUCost([]byte(`{"properties":{"columns":[{"name":"Cost"}],"rows":[]}}`), &out); err == nil {
		t.Fatal("sem PricingModel/Meter deveria ser erro explícito")
	}
}
