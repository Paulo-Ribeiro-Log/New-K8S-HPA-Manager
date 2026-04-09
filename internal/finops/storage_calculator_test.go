package finops

import (
	"testing"
)

// ─── MapStorageClassToAzureType ───────────────────────────────────────────────

func TestMapStorageClassToAzureType(t *testing.T) {
	cases := []struct {
		scName      string
		provisioner string
		skuName     string
		wantType    string
		wantMethod  string
	}{
		// Via skuName explícito (mais confiável)
		{"my-sc", "disk.csi.azure.com", "Premium_LRS", "Premium SSD", "sku_param"},
		{"my-sc", "disk.csi.azure.com", "premium_zrs", "Premium SSD", "sku_param"},
		{"my-sc", "disk.csi.azure.com", "StandardSSD_LRS", "Standard SSD", "sku_param"},
		{"my-sc", "disk.csi.azure.com", "standardssd_zrs", "Standard SSD", "sku_param"},
		{"my-sc", "disk.csi.azure.com", "Standard_LRS", "Standard HDD", "sku_param"},

		// Via provisioner — disk sem skuName cai para name_hint
		{"managed-csi-premium", "disk.csi.azure.com", "", "Premium SSD", "name_hint"},
		{"azurefile-standard", "file.csi.azure.com", "", "Azure Files Standard", "provisioner"},
		{"azurefile-premium", "file.csi.azure.com", "", "Azure Files Premium", "provisioner"},
		{"blobfuse", "blob.csi.azure.com", "", "Azure Blob Hot", "provisioner"},
		{"azurefile-standard", "kubernetes.io/azure-file", "", "Azure Files Standard", "provisioner"},
		{"managed-premium", "kubernetes.io/azure-disk", "", "Premium SSD", "name_hint"},

		// Via nome da StorageClass (hints — ordem importa: "premium" bate antes de "azurefile-premium")
		{"managed-csi-premium", "", "", "Premium SSD", "name_hint"},
		{"standard-ssd-csi", "", "", "Standard SSD", "name_hint"},
		{"managed-csi", "", "", "Standard SSD", "name_hint"}, // AKS default
		// "azurefile-premium" sem provisioner bate em "premium" antes de "azurefile-premium"
		{"azurefile-premium", "", "", "Premium SSD", "name_hint"},
		// "azurefile" sem "premium" → Azure Files Standard
		{"azurefile-csi", "", "", "Azure Files Standard", "name_hint"},
		{"blobfuse-sc", "", "", "Azure Blob Hot", "name_hint"},
		{"managed", "", "", "Standard HDD", "name_hint"},

		// Fallback default
		{"unknown-sc", "", "", "Standard SSD", "default"},
		{"", "", "", "Standard SSD", "default"},
	}

	for _, tc := range cases {
		gotType, gotMethod := MapStorageClassToAzureType(tc.scName, tc.provisioner, tc.skuName)
		if gotType != tc.wantType {
			t.Errorf("MapStorageClassToAzureType(%q, %q, %q) tipo = %q, quer %q",
				tc.scName, tc.provisioner, tc.skuName, gotType, tc.wantType)
		}
		if gotMethod != tc.wantMethod {
			t.Errorf("MapStorageClassToAzureType(%q, %q, %q) método = %q, quer %q",
				tc.scName, tc.provisioner, tc.skuName, gotMethod, tc.wantMethod)
		}
	}
}

// ─── ResolveManagedDiskTier ───────────────────────────────────────────────────

func TestResolveManagedDiskTier(t *testing.T) {
	cases := []struct {
		azureType  string
		capacityGB float64
		wantTier   string
	}{
		// Premium SSD
		{"Premium SSD", 1, "P1"},     // exato mínimo
		{"Premium SSD", 4, "P1"},     // exato P1
		{"Premium SSD", 5, "P2"},     // acima de P1 (4GB) → P2
		{"Premium SSD", 32, "P4"},    // exato P4
		{"Premium SSD", 33, "P6"},    // acima de P4 → P6
		{"Premium SSD", 64, "P6"},    // exato P6
		{"Premium SSD", 100, "P10"},  // entre P6(64) e P10(128) → P10
		{"Premium SSD", 128, "P10"},  // exato P10
		{"Premium SSD", 129, "P15"},  // acima de P10 → P15
		{"Premium SSD", 4096, "P50"}, // exato máximo
		{"Premium SSD", 5000, "P50"}, // acima do máximo → maior tier

		// Standard SSD
		{"Standard SSD", 1, "E1"},
		{"Standard SSD", 32, "E4"},
		{"Standard SSD", 33, "E6"},
		{"Standard SSD", 100, "E10"},
		{"Standard SSD", 128, "E10"},
		{"Standard SSD", 129, "E15"},

		// Standard HDD (começa em S4=32GB)
		{"Standard HDD", 1, "S4"},   // abaixo do mínimo → S4
		{"Standard HDD", 32, "S4"},  // exato S4
		{"Standard HDD", 33, "S6"},  // acima de S4 → S6
		{"Standard HDD", 128, "S10"},

		// Tipo desconhecido → fallback para Standard SSD
		{"Unknown Type", 100, "E10"},
	}

	for _, tc := range cases {
		got := ResolveManagedDiskTier(tc.azureType, tc.capacityGB)
		if got != tc.wantTier {
			t.Errorf("ResolveManagedDiskTier(%q, %.0f) = %q, quer %q",
				tc.azureType, tc.capacityGB, got, tc.wantTier)
		}
	}
}

// ─── Cálculo de custo de PVC ─────────────────────────────────────────────────

// TestPVCCostMath verifica a matemática de custo sem necessitar de k8s ou rede.
// Usa os preços de fallback hardcoded + ResolveManagedDiskTier para simular o fluxo.
func TestPVCCostMath(t *testing.T) {
	rate := 5.20 // taxa USD→BRL do teste

	cases := []struct {
		desc        string
		azureType   string
		capacityGB  float64
		wantTier    string
		wantUSD     float64 // preço do tier no fallback
		wantBRL     float64 // wantUSD * rate, arredondado 2 casas
	}{
		{
			desc:       "Premium SSD 100GB → P10",
			azureType:  "Premium SSD",
			capacityGB: 100,
			wantTier:   "P10",
			wantUSD:    diskFallbackPrices["Premium SSD/P10"], // 19.71
			wantBRL:    round2(diskFallbackPrices["Premium SSD/P10"] * rate),
		},
		{
			desc:       "Premium SSD 33GB → P6",
			azureType:  "Premium SSD",
			capacityGB: 33,
			wantTier:   "P6",
			wantUSD:    diskFallbackPrices["Premium SSD/P6"], // 10.22
			wantBRL:    round2(diskFallbackPrices["Premium SSD/P6"] * rate),
		},
		{
			desc:       "Standard SSD 64GB → E6",
			azureType:  "Standard SSD",
			capacityGB: 64,
			wantTier:   "E6",
			wantUSD:    diskFallbackPrices["Standard SSD/E6"], // 3.84
			wantBRL:    round2(diskFallbackPrices["Standard SSD/E6"] * rate),
		},
		{
			desc:       "Standard HDD 128GB → S10",
			azureType:  "Standard HDD",
			capacityGB: 128,
			wantTier:   "S10",
			wantUSD:    diskFallbackPrices["Standard HDD/S10"], // 5.28
			wantBRL:    round2(diskFallbackPrices["Standard HDD/S10"] * rate),
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			tier := ResolveManagedDiskTier(tc.azureType, tc.capacityGB)
			if tier != tc.wantTier {
				t.Errorf("tier = %q, quer %q", tier, tc.wantTier)
			}

			priceUSD, ok := diskFallbackPrices[tc.azureType+"/"+tier]
			if !ok {
				t.Fatalf("preço fallback não encontrado para %s/%s", tc.azureType, tier)
			}
			if priceUSD != tc.wantUSD {
				t.Errorf("priceUSD = %.2f, quer %.2f", priceUSD, tc.wantUSD)
			}

			monthlyCostBRL := round2(priceUSD * rate)
			if monthlyCostBRL != tc.wantBRL {
				t.Errorf("monthlyCostBRL = %.2f, quer %.2f", monthlyCostBRL, tc.wantBRL)
			}

			t.Logf("  %s → %s: $%.2f USD / R$%.2f BRL/mês", tc.azureType, tier, priceUSD, monthlyCostBRL)
		})
	}
}

// TestAzureFilesAndBlobCostMath verifica custo por GB para Files/Blob
func TestAzureFilesAndBlobCostMath(t *testing.T) {
	rate := 5.20

	cases := []struct {
		filesType  string
		capacityGB float64
	}{
		{"Azure Files Standard", 100},
		{"Azure Files Premium", 50},
		{"Azure Blob Hot", 200},
	}

	for _, tc := range cases {
		pricePerGB, ok := filesAndBlobFallbackPerGB[tc.filesType]
		if !ok {
			t.Errorf("preço fallback não encontrado para %q", tc.filesType)
			continue
		}
		costUSD := round2(tc.capacityGB * pricePerGB)
		costBRL := round2(costUSD * rate)
		if costUSD <= 0 {
			t.Errorf("%s %.0fGB: custo USD = 0", tc.filesType, tc.capacityGB)
		}
		t.Logf("  %s %.0fGB: $%.2f USD / R$%.2f BRL/mês (%.3f/GB)", tc.filesType, tc.capacityGB, costUSD, costBRL, pricePerGB)
	}
}

// ─── Detecção de orfãos e buildStorageSummary ─────────────────────────────────

func TestBuildStorageSummary(t *testing.T) {
	items := []PVCCostItem{
		{
			Namespace:      "ns-a",
			Name:           "data-0",
			StorageClass:   "managed-csi-premium",
			AzureDiskType:  "Premium SSD",
			AzureDiskTier:  "P10",
			CapacityGB:     128,
			MonthlyCostUSD: 19.71,
			MonthlyCostBRL: 102.49,
			Phase:          "Bound",
			ReclaimPolicy:  "Delete",
			WorkloadRef:    "ns-a/app-a",
			IsOrphaned:     false,
		},
		{
			Namespace:      "ns-a",
			Name:           "cache-0",
			StorageClass:   "managed-csi-premium",
			AzureDiskType:  "Premium SSD",
			AzureDiskTier:  "P6",
			CapacityGB:     64,
			MonthlyCostUSD: 10.22,
			MonthlyCostBRL: 53.14,
			Phase:          "Bound",
			ReclaimPolicy:  "Delete",
			WorkloadRef:    "ns-a/app-a",
			IsOrphaned:     false,
		},
		{
			Namespace:      "ns-b",
			Name:           "old-data",
			StorageClass:   "managed-csi",
			AzureDiskType:  "Standard SSD",
			AzureDiskTier:  "E6",
			CapacityGB:     64,
			MonthlyCostUSD: 3.84,
			MonthlyCostBRL: 19.97,
			Phase:          "Released",
			ReclaimPolicy:  "Retain",
			WorkloadRef:    "",
			IsOrphaned:     true, // orfão — sem workload
		},
	}

	s := buildStorageSummary(items)

	// Contagens
	if s.PVCCount != 3 {
		t.Errorf("PVCCount = %d, quer 3", s.PVCCount)
	}
	if s.BoundPVCCount != 2 {
		t.Errorf("BoundPVCCount = %d, quer 2", s.BoundPVCCount)
	}
	if s.OrphanedPVCCount != 1 {
		t.Errorf("OrphanedPVCCount = %d, quer 1", s.OrphanedPVCCount)
	}

	// Custo orfão = custo do "old-data"
	if s.OrphanedCostBRL != 19.97 {
		t.Errorf("OrphanedCostBRL = %.2f, quer 19.97", s.OrphanedCostBRL)
	}

	// Capacidade total
	wantGB := round2(128 + 64 + 64)
	if s.TotalCapacityGB != wantGB {
		t.Errorf("TotalCapacityGB = %.2f, quer %.2f", s.TotalCapacityGB, wantGB)
	}

	// Custo total USD
	wantUSD := round2(19.71 + 10.22 + 3.84)
	if s.TotalMonthlyCostUSD != wantUSD {
		t.Errorf("TotalMonthlyCostUSD = %.2f, quer %.2f", s.TotalMonthlyCostUSD, wantUSD)
	}

	// ByStorageClass: 2 classes distintas
	if len(s.ByStorageClass) != 2 {
		t.Errorf("ByStorageClass tem %d entradas, quer 2", len(s.ByStorageClass))
	}
	// Classe mais cara primeiro
	if s.ByStorageClass[0].StorageClass != "managed-csi-premium" {
		t.Errorf("ByStorageClass[0] = %q, quer managed-csi-premium", s.ByStorageClass[0].StorageClass)
	}

	// ByNamespace
	if len(s.ByNamespace) != 2 {
		t.Errorf("ByNamespace tem %d entradas, quer 2", len(s.ByNamespace))
	}
	wantNsA := round2(102.49 + 53.14)
	if s.ByNamespace["ns-a"] != wantNsA {
		t.Errorf("ByNamespace[ns-a] = %.2f, quer %.2f", s.ByNamespace["ns-a"], wantNsA)
	}

	t.Logf("Total: $%.2f USD / R$%.2f BRL | Orfãos: %d (R$%.2f)", s.TotalMonthlyCostUSD, s.TotalMonthlyCostBRL, s.OrphanedPVCCount, s.OrphanedCostBRL)
	t.Logf("By StorageClass: %+v", s.ByStorageClass)
}

// TestOrphanDetectionWithRetain verifica que PVCs orfãos com Retain são identificados corretamente
func TestOrphanDetectionWithRetain(t *testing.T) {
	items := []PVCCostItem{
		{Namespace: "default", Name: "pvc-1", Phase: "Bound", IsOrphaned: false, WorkloadRef: "default/app", ReclaimPolicy: "Delete", MonthlyCostBRL: 50},
		{Namespace: "default", Name: "pvc-2", Phase: "Released", IsOrphaned: true, WorkloadRef: "", ReclaimPolicy: "Retain", MonthlyCostBRL: 100},
		{Namespace: "default", Name: "pvc-3", Phase: "Released", IsOrphaned: true, WorkloadRef: "", ReclaimPolicy: "Delete", MonthlyCostBRL: 30},
	}

	s := buildStorageSummary(items)

	if s.OrphanedPVCCount != 2 {
		t.Errorf("OrphanedPVCCount = %d, quer 2", s.OrphanedPVCCount)
	}
	wantOrphanCost := round2(100.0 + 30.0)
	if s.OrphanedCostBRL != wantOrphanCost {
		t.Errorf("OrphanedCostBRL = %.2f, quer %.2f", s.OrphanedCostBRL, wantOrphanCost)
	}

	// Verificar que os orfãos com Retain podem ser identificados iterando a lista
	retainOrphans := 0
	for _, item := range items {
		if item.IsOrphaned && item.ReclaimPolicy == "Retain" {
			retainOrphans++
		}
	}
	if retainOrphans != 1 {
		t.Errorf("orfãos com Retain = %d, quer 1", retainOrphans)
	}
}
