package azure

import (
	"math"
	"testing"
)

func TestPoolFromVMSSResourceID(t *testing.T) {
	cases := map[string]string{
		// O Cost Management devolve o ID em minúsculas.
		"/subscriptions/9f66/resourcegroups/mc_rg-oferta-app-prd_akspriv-oferta-prd_brazilsouth/providers/microsoft.compute/virtualmachinescalesets/aks-calculofrete-22930315-vmss": "calculofrete",
		"/subscriptions/9f66/resourceGroups/MC_x/providers/Microsoft.Compute/virtualMachineScaleSets/aks-infratoospot-98765432-vmss":                                                "infratoospot",
		// Nome de pool com dígitos e sem confundir com o id numérico do VMSS.
		"/subscriptions/x/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-mktplac2spot-12345678-vmss": "mktplac2spot",
		// Não é VMSS de node pool.
		"/subscriptions/x/resourcegroups/mc_x/providers/microsoft.compute/virtualmachines/vm-avulsa": "",
		"": "",
	}
	for in, want := range cases {
		if got := PoolFromVMSSResourceID(in); got != want {
			t.Errorf("PoolFromVMSSResourceID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccumulatePoolCost(t *testing.T) {
	body := []byte(`{"properties":{
	  "columns":[{"name":"Cost"},{"name":"PricingModel"},{"name":"ResourceId"},{"name":"Currency"}],
	  "rows":[
	    [15400.5,"Reservation","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-calculofrete-22930315-vmss","BRL"],
	    [140.5,"OnDemand","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-calculofrete-22930315-vmss","BRL"],
	    [266,"SavingsPlan","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-monitoring-11111111-vmss","BRL"],
	    [114,"Spot","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-infratoospot-22222222-vmss","BRL"],
	    [9,"","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachinescalesets/aks-frete-33333333-vmss","BRL"],
	    [999,"OnDemand","/subscriptions/s/resourcegroups/mc_x/providers/microsoft.compute/virtualmachines/vm-avulsa","BRL"]
	  ],
	  "nextLink":"https://management.azure.com/next"}}`)
	var out PoolCost
	out.ByPool = map[string]map[string]float64{}
	next, err := accumulatePoolCost(body, &out)
	if err != nil {
		t.Fatal(err)
	}
	if next != "https://management.azure.com/next" {
		t.Errorf("nextLink perdido: %q", next)
	}
	if out.Currency != "BRL" {
		t.Errorf("moeda = %q", out.Currency)
	}
	if got := out.ByPool["calculofrete"][ModelReservation]; math.Abs(got-15400.5) > 1e-9 {
		t.Errorf("calculofrete/reserva = %v", got)
	}
	if got := out.ByPool["calculofrete"][ModelOnDemand]; math.Abs(got-140.5) > 1e-9 {
		t.Errorf("calculofrete/on-demand = %v", got)
	}
	if out.ByPool["monitoring"][ModelSavingsPlan] != 266 || out.ByPool["infratoospot"][ModelSpot] != 114 {
		t.Errorf("savings plan/spot mal lidos: %+v", out.ByPool)
	}
	if out.ByPool["frete"][ModelOther] != 9 {
		t.Errorf("modelo de preço vazio deve virar 'other', não sumir: %+v", out.ByPool["frete"])
	}
	if _, has := out.ByPool[""]; has || len(out.ByPool) != 4 {
		t.Errorf("a VM avulsa (não é VMSS de node pool) não pode entrar: %+v", out.ByPool)
	}
}

func TestAccumulatePoolCost_SemColunaEsperadaEhErro(t *testing.T) {
	var out PoolCost
	out.ByPool = map[string]map[string]float64{}
	if _, err := accumulatePoolCost([]byte(`{"properties":{"columns":[{"name":"Cost"}],"rows":[]}}`), &out); err == nil {
		t.Fatal("resposta sem PricingModel/ResourceId deveria ser erro explícito (não cobertura vazia silenciosa)")
	}
}
