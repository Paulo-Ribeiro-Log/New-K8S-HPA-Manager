package finops

import "testing"

// Itens no formato REAL devolvido pela Azure Retail Prices API (brazilsouth, capturados ao vivo):
// o mesmo skuName tem, na unidade "1/Month", o disco e a tarifa acessória "Disk Mount".
func TestPickRetailPrice_IgnoresDiskMountMeter(t *testing.T) {
	items := []azurePriceItem{
		{MeterName: "E10 LRS Disk", UnitOfMeasure: "1/Month", RetailPrice: 17.92},
		{MeterName: "E10 LRS Disk Operations", UnitOfMeasure: "10K", RetailPrice: 0.002},
		{MeterName: "E10 LRS Disk Mount", UnitOfMeasure: "1/Month", RetailPrice: 1.82},
	}
	if got := pickRetailPrice(items, "1/Month", "E10 LRS Disk"); got != 17.92 {
		t.Errorf("com o medidor exato deveria dar 17.92 (disco), veio %v", got)
	}
	// Regressão do bug: sem restringir o medidor, o "menor preço" pega o Disk Mount.
	if got := pickRetailPrice(items, "1/Month", ""); got != 1.82 {
		t.Errorf("sem medidor o comportamento antigo (menor preço) é o Disk Mount 1.82, veio %v", got)
	}
	if got := pickRetailPrice(items, "1/Month", "e10 lrs disk"); got != 17.92 {
		t.Errorf("medidor deve casar sem diferenciar caixa: %v", got)
	}
	if got := pickRetailPrice(items, "1/Month", "E99 LRS Disk"); got != 0 {
		t.Errorf("medidor inexistente deve dar 0 (cai no fallback), veio %v", got)
	}
}

func TestPickRetailPrice_HDDHasOnlyDiskMeter(t *testing.T) {
	items := []azurePriceItem{
		{MeterName: "S6 LRS Disk", UnitOfMeasure: "1/Month", RetailPrice: 7.2192},
		{MeterName: "S6 LRS Disk Operations", UnitOfMeasure: "10K", RetailPrice: 0.0005},
	}
	if got := pickRetailPrice(items, "1/Month", "S6 LRS Disk"); got != 7.2192 {
		t.Errorf("S6 = %v", got)
	}
}
