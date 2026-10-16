package azure

import "testing"

func TestAccumulateRGServiceCost(t *testing.T) {
	body := []byte(`{"properties":{
	  "columns":[{"name":"Cost"},{"name":"ResourceGroupName"},{"name":"ServiceName"},{"name":"Currency"}],
	  "rows":[
	    [24878.4,"mc_rg-oferta-app-prd_akspriv-oferta-prd_brazilsouth","Virtual Machines","BRL"],
	    [1200,"MC_rg-oferta-app-prd_akspriv-oferta-prd_brazilsouth","Storage","BRL"],
	    [300,"mc_rg-oferta-app-prd_akspriv-oferta-prd_brazilsouth","Storage","BRL"],
	    [380,"rg-oferta-app-prd","Azure Kubernetes Service","BRL"],
	    [7,"rg-oferta-app-prd","","BRL"],
	    ["x","rg-oferta-app-prd","Bandwidth","BRL"]
	  ]}}`)
	out := RGServiceCost{ByRG: map[string]map[string]float64{}}
	next, err := accumulateRGServiceCost(body, &out)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" {
		t.Errorf("nextLink = %q", next)
	}
	nodes := out.ByRG["mc_rg-oferta-app-prd_akspriv-oferta-prd_brazilsouth"]
	if nodes["Storage"] != 1500 {
		t.Errorf("RG em caixas diferentes deve somar no mesmo grupo: %+v", out.ByRG)
	}
	if nodes["Virtual Machines"] != 24878.4 {
		t.Errorf("VMs = %v", nodes["Virtual Machines"])
	}
	app := out.ByRG["rg-oferta-app-prd"]
	if app["Azure Kubernetes Service"] != 380 || app["Outros"] != 7 {
		t.Errorf("RG do AKS mal lido: %+v", app)
	}
	if _, ok := app["Bandwidth"]; ok {
		t.Errorf("linha com custo inválido deve ser ignorada")
	}
	if out.Currency != "BRL" {
		t.Errorf("moeda = %q", out.Currency)
	}
}

func TestAccumulateRGServiceCost_ColunaFaltando(t *testing.T) {
	out := RGServiceCost{ByRG: map[string]map[string]float64{}}
	if _, err := accumulateRGServiceCost([]byte(`{"properties":{"columns":[{"name":"Cost"}],"rows":[]}}`), &out); err == nil {
		t.Error("esperava erro sem a coluna ResourceGroupName")
	}
}
