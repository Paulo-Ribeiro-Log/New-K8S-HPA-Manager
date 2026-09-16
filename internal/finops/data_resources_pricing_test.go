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

// TestStorageRedundancyFromSKU cobre a extração de redundância do SKU cru — formato real
// confirmado ao vivo (`az resource list`) pras 2 Storage Accounts reais encontradas nesta
// investigação: "Standard_LRS".
func TestStorageRedundancyFromSKU(t *testing.T) {
	cases := map[string]string{
		"Standard_LRS":   "LRS",
		"Standard_GRS":   "GRS",
		"standard_zrs":   "ZRS",
		"Premium_LRS":    "LRS",
		"Standard_RAGRS": "RAGRS",
		"":               "",
	}
	for in, want := range cases {
		if got := storageRedundancyFromSKU(in); got != want {
			t.Errorf("storageRedundancyFromSKU(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPickTieredPrice_RealAzureTiers — os 3 pontos de dado (tierMinimumUnits/retailPrice) são os
// EXATOS valores confirmados ao vivo contra a Retail Prices API real (Storage, região
// brazilsouth, "General Block Blob v2" / "Hot LRS" / "Data Stored") durante a investigação do F3.1
// — não valores inventados.
func TestPickTieredPrice_RealAzureTiers(t *testing.T) {
	items := []retailPriceItem{
		{UnitOfMeasure: "1 GB/Month", TierMinimumUnits: 0, RetailPrice: 0.0326},
		{UnitOfMeasure: "1 GB/Month", TierMinimumUnits: 51200, RetailPrice: 0.03146},
		{UnitOfMeasure: "1 GB/Month", TierMinimumUnits: 512000, RetailPrice: 0.03032},
		// ruído: item de outra unidade de medida, não deve interferir na escolha da faixa.
		{UnitOfMeasure: "10K", TierMinimumUnits: 0, RetailPrice: 0.0056},
	}

	cases := []struct {
		usedGB    float64
		wantPrice float64
		wantOK    bool
	}{
		{2.53, 0.0326, true},     // caso real: stgcdchlg (F3.1), ~2.53GB em uso — 1ª faixa
		{51199.99, 0.0326, true}, // ainda na 1ª faixa, na borda
		{51200, 0.03146, true},   // exatamente no limiar da 2ª faixa
		{600000, 0.03032, true},  // 3ª faixa
		{0, 0.0326, true},        // conta vazia — ainda cai na 1ª faixa (tierMinimumUnits=0)
	}
	for _, c := range cases {
		price, ok := pickTieredPrice(items, c.usedGB)
		if ok != c.wantOK || price != c.wantPrice {
			t.Errorf("pickTieredPrice(usedGB=%v) = (%v, %v), want (%v, %v)", c.usedGB, price, ok, c.wantPrice, c.wantOK)
		}
	}

	if _, ok := pickTieredPrice(nil, 100); ok {
		t.Error("pickTieredPrice(nil, ...) deveria retornar ok=false, nunca inventar um preço")
	}
}

// TestResourceGroupFromID cobre a extração do Resource Group a partir de um Resource ID ARM
// completo — formato real de `az resource list --query [].id`.
func TestResourceGroupFromID(t *testing.T) {
	id := "/subscriptions/be2ab514-98fa-41a5-9a21-3d4928ee6112/resourceGroups/rg-cdc-data-hlg/providers/Microsoft.Storage/storageAccounts/stgcdchlg"
	if got := resourceGroupFromID(id); got != "rg-cdc-data-hlg" {
		t.Errorf("resourceGroupFromID(%q) = %q, want %q", id, got, "rg-cdc-data-hlg")
	}
	if got := resourceGroupFromID(""); got != "" {
		t.Errorf("resourceGroupFromID(\"\") = %q, want \"\"", got)
	}
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
