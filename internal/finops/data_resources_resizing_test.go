package finops

import (
	"errors"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// fakeVMPricer é um CloudPricer determinístico (sem SQLite/rede).
type fakeVMPricer struct {
	specs  map[string][2]int
	prices map[string]float64
}

func (f fakeVMPricer) GetVMSpecs(sku string) (int, int) { s := f.specs[sku]; return s[0], s[1] }
func (f fakeVMPricer) GetPrice(sku string) (float64, string, error) {
	if p, ok := f.prices[sku]; ok {
		return p, "api", nil
	}
	return 0, "", errors.New("sem preço")
}

func vmPricer() fakeVMPricer {
	return fakeVMPricer{
		specs: map[string][2]int{
			"Standard_D2s_v4": {2, 8}, "Standard_D4s_v4": {4, 16}, "Standard_D8s_v4": {8, 32},
			"Standard_D2s_v5": {2, 8},
			"Standard_B2s":    {2, 4}, "Standard_B2ms": {2, 8}, "Standard_B4ms": {4, 16},
		},
		prices: map[string]float64{
			"Standard_D2s_v4": 0.153, "Standard_D4s_v4": 0.306, "Standard_D8s_v4": 0.612,
			"Standard_D2s_v5": 0.14,
			"Standard_B2s":    0.052, "Standard_B2ms": 0.104, "Standard_B4ms": 0.208,
		},
	}
}

func dataVM(sku string) AzureDataResource {
	return AzureDataResource{Name: "vm", Type: "Microsoft.Compute/virtualMachines", SKUName: sku, Location: "brazilsouth", MonthlyCostUSD: 111.69}
}

func TestVMUsage_OversizedFourVCPUHalves(t *testing.T) {
	u := DataUtilization{Days: 14, Points: 300, CPUAvgPct: 4, CPUP95Pct: 9, CPUMaxPct: 30, HasMemory: true, MemAvgPct: 18, MemP95Pct: 22, MemMaxPct: 25}
	r := dataVM("Standard_D4s_v4")
	ApplyUsageRecommendations(&r, u, "rg-x-data-hlg", vmPricer(), nil, 5)
	if r.Utilization == nil {
		t.Fatal("Utilization deveria ser preenchida")
	}
	var found *DataRecommendation
	for i := range r.Recommendations {
		if r.Recommendations[i].Kind == "vm_resize" {
			found = &r.Recommendations[i]
		}
	}
	if found == nil || found.TargetSKU != "Standard_D2s_v5" || found.Verdict != "recommended" || found.MonthlySavingsBRL <= 0 {
		t.Fatalf("esperava reduzir D4→D2s_v5 (geração mais nova, recommended em HLG) com economia: %+v", r.Recommendations)
	}
	if !strings.Contains(found.Reason, "P95 9%") {
		t.Errorf("a evidência (uso real) precisa estar na razão: %q", found.Reason)
	}
}

func TestVMUsage_ProdIsOnlyConsiderWithCaution(t *testing.T) {
	u := DataUtilization{Days: 14, CPUAvgPct: 4, CPUP95Pct: 9, HasMemory: true, MemP95Pct: 22, MemMaxPct: 25}
	r := dataVM("Standard_D4s_v4")
	ApplyUsageRecommendations(&r, u, "rg-x-data-prd", vmPricer(), nil, 5)
	rec := r.Recommendations[0]
	if rec.Verdict != "consider" || !strings.Contains(rec.Reason, "PRD") {
		t.Errorf("em PRD a oferta é 'consider' e avisa pra validar: %+v", rec)
	}
}

func TestVMUsage_TwoVCPUGetsBurstableNotHalving(t *testing.T) {
	// 2 vCPU não tem "metade" na família D — a saída é Burstable, escolhida pela memória de pico.
	u := DataUtilization{Days: 14, CPUAvgPct: 3.6, CPUP95Pct: 8, CPUMaxPct: 75, HasMemory: true, MemAvgPct: 21, MemP95Pct: 24, MemMaxPct: 30}
	r := dataVM("Standard_D2s_v4")
	ApplyUsageRecommendations(&r, u, "rg-x-data-hlg", vmPricer(), nil, 5)
	if len(r.Recommendations) != 1 || r.Recommendations[0].Kind != "vm_burstable" {
		t.Fatalf("esperava 1 oferta Burstable: %+v", r.Recommendations)
	}
	rec := r.Recommendations[0]
	// pico de memória 30% de 8 GB = 2,4 GB + 25% de folga = 3 GB → B2s (4 GB) basta e é a mais barata
	if rec.TargetSKU != "Standard_B2s" {
		t.Errorf("alvo = %s, quer Standard_B2s (cabe a memória de pico)", rec.TargetSKU)
	}
	if want := round2((0.153 - 0.052) * hoursPerMonth * 5); rec.MonthlySavingsBRL != want {
		t.Errorf("economia = %v, quer %v", rec.MonthlySavingsBRL, want)
	}
}

func TestVMUsage_BurstableRespectsMemoryPeak(t *testing.T) {
	// Pico de memória 70% de 8 GB = 5,6 GB (+25% = 7 GB): B2s (4 GB) não cabe → B2ms (8 GB).
	u := DataUtilization{Days: 14, CPUAvgPct: 3, CPUP95Pct: 8, HasMemory: true, MemP95Pct: 60, MemMaxPct: 70}
	r := dataVM("Standard_D2s_v4")
	ApplyUsageRecommendations(&r, u, "rg-x-data-hlg", vmPricer(), nil, 5)
	if len(r.Recommendations) != 1 || r.Recommendations[0].TargetSKU != "Standard_B2ms" {
		t.Errorf("esperava B2ms: %+v", r.Recommendations)
	}
}

func TestVMUsage_NoOfferWhenBusyOrMemoryUnknown(t *testing.T) {
	busy := dataVM("Standard_D4s_v4")
	ApplyUsageRecommendations(&busy, DataUtilization{Days: 14, CPUAvgPct: 60, CPUP95Pct: 85, HasMemory: true, MemP95Pct: 40}, "rg-x-data-hlg", vmPricer(), nil, 5)
	if len(busy.Recommendations) != 1 || busy.Recommendations[0].Kind != "vm_undersized" || busy.Recommendations[0].MonthlySavingsBRL != 0 {
		t.Errorf("VM ocupada não recebe oferta de redução: %+v", busy.Recommendations)
	}

	// Sem métrica de memória NUNCA se afirma que a VM está sobrando.
	noMem := dataVM("Standard_D4s_v4")
	ApplyUsageRecommendations(&noMem, DataUtilization{Days: 14, CPUAvgPct: 2, CPUP95Pct: 5, HasMemory: false}, "rg-x-data-hlg", vmPricer(), nil, 5)
	for _, rec := range noMem.Recommendations {
		if rec.Verdict != "info" || rec.MonthlySavingsBRL != 0 {
			t.Errorf("sem memória só pode haver aviso informativo: %+v", rec)
		}
	}

	// SKU fora da tabela de specs: aviso honesto, sem inventar alternativa.
	unknown := dataVM("Standard_D4ls_v5")
	ApplyUsageRecommendations(&unknown, DataUtilization{Days: 14, CPUP95Pct: 5, HasMemory: true}, "rg-x-data-hlg", vmPricer(), nil, 5)
	if len(unknown.Recommendations) != 1 || unknown.Recommendations[0].Verdict != "info" || !strings.Contains(unknown.Recommendations[0].Reason, "tabela de specs") {
		t.Errorf("SKU desconhecido: %+v", unknown.Recommendations)
	}
}

func TestHalveFlexSKU(t *testing.T) {
	cases := map[string]string{"Standard_D4ds_v5": "Standard_D2ds_v5", "Standard_E8ds_v5": "Standard_E4ds_v5", "Standard_D48ds_v5": "Standard_D32ds_v5"}
	for in, want := range cases {
		if got, ok := halveFlexSKU(in); !ok || got != want {
			t.Errorf("halveFlexSKU(%s) = %q,%v; quer %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"Standard_D2ds_v5", "Standard_B1ms", "lixo"} {
		if _, ok := halveFlexSKU(in); ok {
			t.Errorf("%s não tem degrau abaixo", in)
		}
	}
}

func flexServer(sku, tier string, usd float64) AzureDataResource {
	return AzureDataResource{Name: "pg", Type: "Microsoft.DBforPostgreSQL/flexibleServers", SKUName: sku, SKUTier: tier, Location: "brazilsouth", MonthlyCostUSD: usd}
}

func TestFlexUsage_GeneralPurposeGoesBurstable(t *testing.T) {
	price := func(t AzureDataResource) (float64, bool) {
		if t.SKUName == "Standard_B2ms" && t.SKUTier == "Burstable" {
			return 60, true
		}
		return 0, false
	}
	u := DataUtilization{Days: 14, CPUAvgPct: 5, CPUP95Pct: 12, HasMemory: true, MemAvgPct: 25, MemP95Pct: 35, StoragePct: 30}
	r := flexServer("Standard_D2ds_v5", "GeneralPurpose", 175.2)
	ApplyUsageRecommendations(&r, u, "rg-x-data-hlg", nil, price, 5)
	if len(r.Recommendations) != 1 || r.Recommendations[0].Kind != "flex_resize" || r.Recommendations[0].TargetSKU != "Standard_B2ms" {
		t.Fatalf("esperava oferta B2ms: %+v", r.Recommendations)
	}
	if want := round2((175.2 - 60) * 5); r.Recommendations[0].MonthlySavingsBRL != want {
		t.Errorf("economia = %v, quer %v", r.Recommendations[0].MonthlySavingsBRL, want)
	}
}

func TestFlexUsage_StorageAlertAndNoOfferWhenPricier(t *testing.T) {
	price := func(AzureDataResource) (float64, bool) { return 500, true } // alvo MAIS caro: sem oferta
	u := DataUtilization{Days: 14, CPUP95Pct: 10, HasMemory: true, MemP95Pct: 20, StoragePct: 91}
	r := flexServer("Standard_D2ds_v5", "GeneralPurpose", 175.2)
	ApplyUsageRecommendations(&r, u, "rg-x-data-prd", nil, price, 5)
	if len(r.Recommendations) != 1 || r.Recommendations[0].Kind != "flex_storage" {
		t.Errorf("storage 91%% gera alerta e alvo mais caro não gera oferta: %+v", r.Recommendations)
	}
	// B1ms (Burstable, menor degrau): nada a oferecer.
	b := flexServer("Standard_B1ms", "Burstable", 15)
	ApplyUsageRecommendations(&b, DataUtilization{Days: 14, CPUP95Pct: 5, HasMemory: true, MemP95Pct: 20}, "rg-x-data-hlg", nil, price, 5)
	if len(b.Recommendations) != 0 {
		t.Errorf("B1ms não tem oferta: %+v", b.Recommendations)
	}
}

func TestInventory_DiskFindings(t *testing.T) {
	std := func(diskType, tier, region string) (float64, string, error) {
		if diskType == "Standard SSD" && tier == "E20" {
			return 40, "api", nil
		}
		return 0, "", errors.New("preço inesperado")
	}
	res := []AzureDataResource{
		{Name: "vm-on", Type: "Microsoft.Compute/virtualMachines", SKUName: "Standard_D2s_v4"},
		{Name: "vm-off", Type: "Microsoft.Compute/virtualMachines", PowerState: "deallocated"},
		{Name: "orfao", Type: "Microsoft.Compute/disks", DiskState: "Unattached", MonthlyCostBRL: 90, UnattachedSince: "2026-05-01T10:00:00Z"},
		{Name: "off-data", Type: "Microsoft.Compute/disks", DiskState: "Reserved", AttachedTo: "vm-off", MonthlyCostBRL: 50},
		{Name: "off-os", Type: "Microsoft.Compute/disks", DiskState: "Reserved", AttachedTo: "VM-OFF", MonthlyCostBRL: 10},
		{Name: "prem", Type: "Microsoft.Compute/disks", SKUName: "Premium_LRS", SizeGB: 512, DiskTier: "P20", DiskState: "Attached", AttachedTo: "vm-on", MonthlyCostUSD: 100, MonthlyCostBRL: 500, Location: "brazilsouth"},
	}

	ApplyInventoryRecommendations(res, "rg-x-data-hlg", std, 5)
	by := map[string]AzureDataResource{}
	for _, r := range res {
		by[r.Name] = r
	}

	if r := by["orfao"].Recommendations; len(r) != 1 || r[0].Kind != "disk_unattached" || r[0].MonthlySavingsBRL != 90 || !strings.Contains(r[0].Reason, "desde 2026-05-01") {
		t.Errorf("disco desatachado: %+v", r)
	}
	if r := by["off-data"].Recommendations; len(r) != 1 || r[0].Kind != "disk_deallocated_vm" || r[0].MonthlySavingsBRL != 0 {
		t.Errorf("disco de VM desalocada: %+v", r)
	}
	// A VM desalocada diz quanto os discos dela ainda custam (50 + 10, casando nome sem diferenciar caixa).
	if r := by["vm-off"].Recommendations; len(r) != 1 || r[0].Kind != "vm_deallocated" || !strings.Contains(r[0].Reason, "2 disco(s)") || !strings.Contains(r[0].Reason, "R$ 60.00") {
		t.Errorf("VM desalocada: %+v", r)
	}
	if r := by["prem"].Recommendations; len(r) != 1 || r[0].Kind != "disk_sku" || r[0].Verdict != "consider" || r[0].MonthlySavingsBRL != 300 {
		t.Errorf("Premium em HLG (100→40 USD ×5 = 300): %+v", r)
	}
	if len(by["vm-on"].Recommendations) != 0 {
		t.Errorf("VM ligada não tem oferta de inventário: %+v", by["vm-on"].Recommendations)
	}

	// Em PRD a troca Premium→Standard NUNCA é sugerida pelo inventário.
	ApplyInventoryRecommendations(res, "rg-x-data-prd", std, 5)
	for _, r := range res {
		if r.Name == "prem" && len(r.Recommendations) != 0 {
			t.Errorf("PRD não sugere trocar Premium por Standard: %+v", r.Recommendations)
		}
	}
}

func TestBuildDataResources_DeallocatedVMFromReservedDisk(t *testing.T) {
	vms := []azure.RGVM{{ID: "/v/on", Name: "vm-on", VMSize: "Standard_D2s_v4", Location: "brazilsouth"}, {ID: "/v/off", Name: "VM-Off", VMSize: "Standard_D2s_v4", Location: "brazilsouth"}}
	disks := []azure.RGDisk{
		{ID: "/d/1", Name: "d1", SKU: "Premium_LRS", SKUTier: "Premium", SizeGB: 512, State: "Attached", ManagedBy: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-on"},
		{ID: "/d/2", Name: "d2", SKU: "StandardSSD_LRS", SizeGB: 128, State: "Reserved", ManagedBy: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-off"},
		{ID: "/d/3", Name: "d3", SKU: "Standard_LRS", SizeGB: 64, State: "Unattached", PerformanceTier: "S10"},
	}
	out := buildDataResources(nil, vms, disks)
	by := map[string]AzureDataResource{}
	for _, r := range out {
		by[r.Name] = r
	}
	if by["VM-Off"].PowerState != "deallocated" || by["vm-on"].PowerState != "" {
		t.Errorf("power state: on=%q off=%q", by["vm-on"].PowerState, by["VM-Off"].PowerState)
	}
	if d := by["d1"]; d.DiskTier != "P20" || d.AttachedTo != "vm-on" || d.DiskState != "Attached" || d.SKUTier != "Premium" {
		t.Errorf("disco Premium 512GB → tier P20: %+v", d)
	}
	if d := by["d2"]; d.DiskTier != "E10" {
		t.Errorf("StandardSSD 128GB → E10: %+v", d)
	}
	if d := by["d3"]; d.DiskTier != "S10" {
		t.Errorf("tier explícito (properties.tier) prevalece sobre o do tamanho: %+v", d)
	}
}

func TestPriceManagedDisk_PremiumV2UsesTableNotPremiumTier(t *testing.T) {
	r := &AzureDataResource{Name: "d", Type: "Microsoft.Compute/disks", SKUName: "PremiumV2_LRS", SizeGB: 1200, ProvisionedIOPS: 3000, ProvisionedMBps: 125, Location: "brazilsouth"}
	priceManagedDisk(r, nil, "brazilsouth", 5) // nil pricer: a tabela não pode depender dele
	if r.PriceSource != "table" || r.MonthlyCostUSD != round2(1200*0.000208*hoursPerMonth) {
		t.Errorf("Premium SSD v2 1200GB no baseline = só capacidade: %+v", r)
	}
}

func TestPriceVirtualMachine_DeallocatedHasNoComputeCost(t *testing.T) {
	r := &AzureDataResource{Name: "vm", Type: "Microsoft.Compute/virtualMachines", SKUName: "Standard_D2s_v4", PowerState: "deallocated"}
	priceVirtualMachine(r, nil, 5)
	if r.MonthlyCostUSD != 0 || r.PricingNote == "" {
		t.Errorf("VM desalocada não cobra compute e explica por quê: %+v", r)
	}
}

// Itens no formato REAL da Retail Prices API (brazilsouth): vários produtos compartilham o medidor
// "Storage Data Stored"; só o de Flexible Server em GB/mês é o certo.
func TestPickFlexStoragePrice_OnlyFlexServerGBMonth(t *testing.T) {
	items := []retailPriceItem{
		{ProductName: "Azure Database for PostgreSQL Flex Server Storage", MeterName: "Storage Data Stored", UnitOfMeasure: "1 GB/Month", RetailPrice: 0.2185},
		{ProductName: "Azure Database for PostgreSQL Single Server General Purpose - Storage", MeterName: "Storage Data Stored", UnitOfMeasure: "1 GB/Month", RetailPrice: 0.05},
		{ProductName: "Azure Database for PostgreSQL Flex Server Storage", MeterName: "Storage Data Stored", UnitOfMeasure: "1 GiB/Month", RetailPrice: 0.01},
	}
	if got := pickFlexStoragePrice(items); got != 0.2185 {
		t.Errorf("preço do storage Flex = %v, quer 0.2185 (ignora Single Server e outra unidade)", got)
	}
	if got := pickFlexStoragePrice(nil); got != 0 {
		t.Errorf("sem itens deve dar 0, veio %v", got)
	}
}

func TestFlexTotalCost_StorageDominatesOnSmallBurstable(t *testing.T) {
	// pgsh-adanalytics-1 (real): B1ms com 512 GB — o storage vale bem mais que o compute.
	compute, storage, total := flexTotalCost(20, 512, 0.2185, false)
	if compute != 20 || storage != 111.87 || total != 131.87 {
		t.Errorf("sem HA: compute=%v storage=%v total=%v", compute, storage, total)
	}
	if storage <= compute {
		t.Error("cenário real: o storage precisa dominar o compute")
	}
	// Com alta disponibilidade (standby) tudo dobra.
	c2, s2, t2 := flexTotalCost(20, 512, 0.2185, true)
	if c2 != 40 || s2 != 223.74 || t2 != 263.74 {
		t.Errorf("com HA: compute=%v storage=%v total=%v", c2, s2, t2)
	}
	// Sem preço de storage o total é só compute (e a nota avisa que está subestimado).
	if _, st, tt := flexTotalCost(20, 512, 0, false); st != 0 || tt != 20 {
		t.Errorf("sem preço de storage: storage=%v total=%v", st, tt)
	}
}

func TestFlexPricingNote_ExplainsWhatIsAndIsNotIncluded(t *testing.T) {
	ok := flexPricingNote(512, true, false, "")
	if !strings.Contains(ok, "storage provisionado (512 GB)") || strings.Contains(ok, "SUBESTIMADO") {
		t.Errorf("com storage precificado: %q", ok)
	}
	if n := flexPricingNote(512, false, false, ""); !strings.Contains(n, "SUBESTIMADO") {
		t.Errorf("storage não precificado precisa avisar subestimativa: %q", n)
	}
	if n := flexPricingNote(256, true, true, ""); !strings.Contains(n, "em dobro") {
		t.Errorf("HA precisa aparecer na nota: %q", n)
	}
}

func TestMergeFlexServers_CaseInsensitiveByResourceID(t *testing.T) {
	res := []AzureDataResource{
		{Name: "pg", Type: "Microsoft.DBforPostgreSQL/flexibleServers", ResourceID: "/subscriptions/S/resourceGroups/RG-X-DATA-HLG/providers/Microsoft.DBforPostgreSQL/flexibleServers/pg"},
		{Name: "outro", Type: "Microsoft.Storage/storageAccounts", ResourceID: "/x"},
	}
	flex := []azure.RGFlexServer{{ID: "/subscriptions/s/resourcegroups/rg-x-data-hlg/providers/microsoft.dbforpostgresql/flexibleservers/pg", StorageGB: 512, StorageTier: "P20", IOPS: 2300, HAMode: "ZoneRedundant"}}
	mergeFlexServers(res, flex)
	if r := res[0]; r.StorageGB != 512 || r.StorageTier != "P20" || r.ProvisionedIOPS != 2300 || r.HAMode != "ZoneRedundant" {
		t.Errorf("merge por ID sem diferenciar caixa: %+v", r)
	}
	if res[1].StorageGB != 0 {
		t.Error("recurso sem flex correspondente não pode receber storage")
	}
}

func TestFlexUsage_SavingsComparesComputeOnlyNotStorage(t *testing.T) {
	// MonthlyCost (500) inclui 400 de storage; o compute é 100. Trocar o SKU só mexe no compute:
	// alvo de 30 economiza 70 (×5), e NÃO 470 (500 − 30) como um cálculo ingênuo daria.
	price := func(AzureDataResource) (float64, bool) { return 30, true }
	r := flexServer("Standard_D2ds_v5", "GeneralPurpose", 500)
	r.ComputeCostUSD = 100
	u := DataUtilization{Days: 14, CPUAvgPct: 5, CPUP95Pct: 12, HasMemory: true, MemP95Pct: 30, StoragePct: 20}
	ApplyUsageRecommendations(&r, u, "rg-x-data-hlg", nil, price, 5)
	if len(r.Recommendations) != 1 || r.Recommendations[0].MonthlySavingsBRL != 350 {
		t.Errorf("economia deve ser (100−30)×5 = 350: %+v", r.Recommendations)
	}
}

// Itens no formato REAL da Retail API: o skuName Burstable vem em caixa alta ("B1MS") e a busca
// antiga (`contains(skuName, 'B1ms')`, sensível a caixa) nunca os achava — 14 dos 20 Flexible
// Servers reais da frota ficavam sem preço.
func TestPickFlexComputeHourly_BurstableCaseInsensitive(t *testing.T) {
	burstable := []retailPriceItem{
		{ProductName: "Azure Database for PostgreSQL Flexible Server Burstable BS Series Compute", SKUName: "B1MS", UnitOfMeasure: "1 Hour", RetailPrice: 0.035},
		{ProductName: "Azure Database for PostgreSQL Flexible Server Burstable BS Series Compute", SKUName: "B2S", UnitOfMeasure: "1 Hour", RetailPrice: 0.14},
		{ProductName: "Azure Database for PostgreSQL Flexible Server Burstable BS Series Compute", SKUName: "B2ms", UnitOfMeasure: "1 Hour", RetailPrice: 0.28},
		{ProductName: "Azure Database for PostgreSQL Flexible Server Burstable BS Series Compute", SKUName: "Basic", UnitOfMeasure: "1 Hour", RetailPrice: 0.035},
	}
	for sku, want := range map[string]float64{"B1ms": 0.035, "B2s": 0.14, "B2ms": 0.28} {
		if got := pickFlexComputeHourly(burstable, false, 0, sku); got != want {
			t.Errorf("%s = %v, quer %v", sku, got, want)
		}
	}
	// MySQL grava com o prefixo "Standard_".
	my := []retailPriceItem{{SKUName: "Standard_B4ms", UnitOfMeasure: "1 Hour", RetailPrice: 0.56}}
	if got := pickFlexComputeHourly(my, false, 0, "B4ms"); got != 0.56 {
		t.Errorf("MySQL Standard_B4ms = %v", got)
	}
	// SKU inexistente: 0 (o chamador explica que o compute não foi estimado).
	if got := pickFlexComputeHourly(burstable, false, 0, "B99ms"); got != 0 {
		t.Errorf("B99ms deveria dar 0, veio %v", got)
	}
}

func TestPickFlexComputeHourly_GeneralPurposeByVCores(t *testing.T) {
	gp := []retailPriceItem{
		{SKUName: "1 vCore", UnitOfMeasure: "1 Hour", RetailPrice: 0.12},
		{SKUName: "2 vCore", UnitOfMeasure: "1 Hour", RetailPrice: 0.24},
		{SKUName: "2 vCore", UnitOfMeasure: "1/Month", RetailPrice: 99},
	}
	if got := pickFlexComputeHourly(gp, true, 2, "D2ds_v5"); got != 0.24 {
		t.Errorf("D2ds_v5 (2 vCore) = %v, quer 0.24 (só unidade 1 Hour)", got)
	}
}

func TestFlexPricingNote_ComputeMissingKeepsStorage(t *testing.T) {
	n := flexPricingNote(512, true, false, "Nenhum preço de compute encontrado.")
	if !strings.Contains(n, "SÓ storage") || !strings.Contains(n, "512 GB") {
		t.Errorf("compute sem preço mas storage precificado: %q", n)
	}
}
