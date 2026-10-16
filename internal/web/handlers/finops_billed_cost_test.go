package handlers

import (
	"testing"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

func TestBuildBilledCost(t *testing.T) {
	cost := azure.RGServiceCost{Currency: "BRL", ByRG: map[string]map[string]float64{
		"mc_rg-x_aks-x_brazilsouth": {"Virtual Machines": 1000, "Storage": 200, "Load Balancer": 0.001},
		"rg-x":                      {"Azure Kubernetes Service": 50, "Storage": 10},
		"rg-outro":                  {"Virtual Machines": 9999},
	}}
	r := buildBilledCost("aks-x", cost, "RG-X", "MC_rg-x_aks-x_brazilsouth")
	if !r.Available || r.Currency != "BRL" {
		t.Fatalf("resposta inválida: %+v", r)
	}
	if r.Total < 1260 || r.Total > 1260.01 {
		t.Errorf("total = %v, esperado 1260 (sem o RG que não é do cluster)", r.Total)
	}
	if len(r.ResourceGroups) != 2 || r.ResourceGroups[0].Role != "cluster" || r.ResourceGroups[0].Total != 60 {
		t.Errorf("RG do cluster (nome em maiúsculas na config) mal lido: %+v", r.ResourceGroups)
	}
	if r.Services[0].Service != "Virtual Machines" || r.Services[1].Service != "Storage" || r.Services[1].Cost != 210 {
		t.Errorf("serviços somados entre RGs, maior primeiro: %+v", r.Services)
	}
	for _, s := range r.Services {
		if s.Service == "Load Balancer" {
			t.Errorf("valor abaixo de 1 centavo deve sumir da lista: %+v", r.Services)
		}
	}
}
