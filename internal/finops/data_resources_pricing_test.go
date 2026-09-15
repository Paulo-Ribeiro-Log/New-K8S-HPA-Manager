package finops

import "testing"

func TestParseFlexServerFamily(t *testing.T) {
	cases := []struct {
		sku        string
		wantFamily string
		wantVCores int
		wantOK     bool
	}{
		{"D2ds_v5", "Ddsv5", 2, true}, // real: pgsh-abastecimento-1 (rg-abastecimento-data-hlg)
		{"E4s_v3", "Esv3", 4, true},
		{"D4s_v3", "Dsv3", 4, true},
		// M-series TAMBÉM casa a regex (família "Mdsv2" bate certo com o productName real do
		// catálogo, confirmado ao vivo) — só o skuName do catálogo é diferente pra essa família
		// (nome cru "M64ds_v2", não "64 vCore"), coberto pelo fallback de match em
		// priceFlexibleServer, não por esta função. Só Burstable ("B4ms", sem sufixo "_v") de
		// fato não casa a regex.
		{"M64ds_v2", "Mdsv2", 64, true},
		{"B4ms", "", 0, false},
	}
	for _, c := range cases {
		family, vcores, ok := parseFlexServerFamily(c.sku)
		if ok != c.wantOK || (ok && (family != c.wantFamily || vcores != c.wantVCores)) {
			t.Errorf("parseFlexServerFamily(%q) = (%q, %d, %v), want (%q, %d, %v)", c.sku, family, vcores, ok, c.wantFamily, c.wantVCores, c.wantOK)
		}
	}
}

// TestPriceVirtualMachine_LiveAPI valida priceVirtualMachine contra a Azure Retail Prices API
// REAL, usando o EXATO vmSize encontrado ao vivo num recurso de produção real desta empresa
// (VM "mdbh-abastece-1b", rg-abastecimento-data-hlg — MongoDB self-hosted) durante o
// desenvolvimento desta feature. Requer rede/API real — mesmo padrão já usado por
// TestCalculatePoolCosts (calculator_test.go), que também faz chamada de rede real nesta suíte.
func TestPriceVirtualMachine_LiveAPI(t *testing.T) {
	pricer, err := NewAzurePricer("brazilsouth")
	if err != nil {
		t.Fatalf("criar pricer: %v", err)
	}
	defer pricer.Close()

	r := &AzureDataResource{Name: "mdbh-abastece-1b", Type: "Microsoft.Compute/virtualMachines", SKUName: "Standard_D2s_v4", Location: "brazilsouth"}
	priceVirtualMachine(r, pricer, 5.2)

	if r.MonthlyCostUSD <= 0 {
		t.Fatalf("esperava MonthlyCostUSD > 0 pra Standard_D2s_v4, veio %v (nota: %s)", r.MonthlyCostUSD, r.PricingNote)
	}
	t.Logf("Standard_D2s_v4: $%.2f/mês (R$%.2f/mês)", r.MonthlyCostUSD, r.MonthlyCostBRL)
}

// TestPriceManagedDisk_LiveAPI idem, com o disco OS real da mesma VM (Standard_LRS, 64GB).
func TestPriceManagedDisk_LiveAPI(t *testing.T) {
	diskPricer, err := NewDiskPricer("brazilsouth")
	if err != nil {
		t.Fatalf("criar disk pricer: %v", err)
	}
	defer diskPricer.Close()

	r := &AzureDataResource{Name: "mdbh-abastece-1-os", Type: "Microsoft.Compute/disks", SKUName: "Standard_LRS", SizeGB: 64, Location: "brazilsouth"}
	priceManagedDisk(r, diskPricer, "brazilsouth", 5.2)

	if r.MonthlyCostUSD <= 0 {
		t.Fatalf("esperava MonthlyCostUSD > 0 pra disco Standard_LRS 64GB, veio %v (nota: %s)", r.MonthlyCostUSD, r.PricingNote)
	}
	t.Logf("Standard_LRS 64GB: $%.2f/mês (R$%.2f/mês)", r.MonthlyCostUSD, r.MonthlyCostBRL)
}

// TestPriceFlexibleServer_LiveAPI idem, com o Azure Database for PostgreSQL Flexible Server real
// encontrado no mesmo RG (pgsh-abastecimento-1, SKU Standard_D2ds_v5, General Purpose).
func TestPriceFlexibleServer_LiveAPI(t *testing.T) {
	r := &AzureDataResource{
		Name: "pgsh-abastecimento-1", Type: "Microsoft.DBforPostgreSQL/flexibleServers",
		SKUName: "Standard_D2ds_v5", SKUTier: "GeneralPurpose", Location: "brazilsouth",
	}
	priceFlexibleServer(r, "brazilsouth", 5.2)

	if r.MonthlyCostUSD <= 0 {
		t.Fatalf("esperava MonthlyCostUSD > 0 pra PostgreSQL Flexible Server Standard_D2ds_v5, veio %v (nota: %s)", r.MonthlyCostUSD, r.PricingNote)
	}
	t.Logf("PostgreSQL Flexible Server Standard_D2ds_v5 (compute apenas): $%.2f/mês (R$%.2f/mês)", r.MonthlyCostUSD, r.MonthlyCostBRL)
}
