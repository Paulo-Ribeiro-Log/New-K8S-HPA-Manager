package azure

import "testing"

// Fixture no formato REAL da ARM REST API (capturado de uma listagem de subscription): note o
// "LastOwnershipUpdateTime" com L maiúsculo dentro de properties, tags do driver CSI e o resource
// group MC_ (node resource group do AKS) em caixa alta.
const armPageFixture = `{
  "value": [
    {"id":"/subscriptions/s/resourceGroups/MC_RG-APP-HLG_AKSPRIV-APP-HLG_BRAZILSOUTH/providers/Microsoft.Compute/disks/pvc-5a75",
     "name":"pvc-5a75","location":"brazilsouth","zones":["3"],"type":"Microsoft.Compute/disks",
     "sku":{"name":"Standard_LRS","tier":"Standard"},
     "tags":{"kubernetes.io-created-for-pv-name":"pvc-5a75","kubernetes.io-created-for-pvc-name":"data-db-0","kubernetes.io-created-for-pvc-namespace":"db","k8s-azure-created-by":"kubernetes"},
     "properties":{"diskState":"Unattached","diskSizeGB":32,"timeCreated":"2026-05-21T19:47:06.6484034+00:00","LastOwnershipUpdateTime":"2026-09-18T22:32:08.7096507+00:00"}},
    {"id":"/subscriptions/s/resourceGroups/rg-x/providers/Microsoft.Compute/disks/manual","name":"manual","location":"eastus",
     "sku":{"name":"StandardSSD_LRS"},"tags":null,
     "properties":{"diskState":"Unattached","diskSizeGB":10,"timeCreated":"2025-01-01T00:00:00+00:00"}},
    {"id":"/subscriptions/s/resourceGroups/rg-x/providers/Microsoft.Compute/disks/anexado","name":"anexado","location":"eastus",
     "sku":{"name":"Premium_LRS"},"properties":{"diskState":"Attached","diskSizeGB":128}},
    {"id":"/subscriptions/s/resourceGroups/rg-x/providers/Microsoft.Compute/disks/reservado","name":"reservado","location":"eastus",
     "sku":{"name":"Premium_LRS"},"properties":{"diskState":"Reserved","diskSizeGB":128}}
  ],
  "nextLink": "https://management.azure.com/subscriptions/s/providers/Microsoft.Compute/disks?api-version=2023-04-02&$skiptoken=abc"
}`

func TestParseARMDisksPage_KeepsOnlyUnattached(t *testing.T) {
	disks, next, err := parseARMDisksPage([]byte(armPageFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if next == "" {
		t.Error("nextLink deveria ser propagado")
	}
	if len(disks) != 2 {
		t.Fatalf("só Unattached entra (Attached/Reserved não): %d discos", len(disks))
	}

	k8s := disks[0]
	if k8s.Provider != "azure" || k8s.SizeGB != 32 || k8s.DiskType != "Standard_LRS" || k8s.Location != "brazilsouth" {
		t.Errorf("campos básicos errados: %+v", k8s)
	}
	if k8s.ResourceGroup != "MC_RG-APP-HLG_AKSPRIV-APP-HLG_BRAZILSOUTH" || k8s.K8sClusterHint != k8s.ResourceGroup {
		t.Errorf("resource group / pista de cluster: rg=%q hint=%q", k8s.ResourceGroup, k8s.K8sClusterHint)
	}
	if k8s.K8sPVCName != "data-db-0" || k8s.K8sPVCNamespace != "db" || k8s.K8sPVName != "pvc-5a75" {
		t.Errorf("tags do K8s não extraídas: %+v", k8s)
	}
	// Regressão do achado ao vivo: a API devolve "LastOwnershipUpdateTime" (L maiúsculo).
	if k8s.UnattachedSince != "2026-09-18T22:32:08.7096507+00:00" {
		t.Errorf("LastOwnershipUpdateTime não lido: %q", k8s.UnattachedSince)
	}
	if !k8s.IsK8sProvisioned() {
		t.Error("deveria ser reconhecido como provisionado pelo K8s")
	}

	manual := disks[1]
	if manual.IsK8sProvisioned() || manual.UnattachedSince != "" || manual.K8sClusterHint != "" {
		t.Errorf("disco manual não deveria ter marcas de K8s nem unattached_since: %+v", manual)
	}
}

func TestParseARMDisksPage_TagsCaseInsensitive(t *testing.T) {
	body := []byte(`{"value":[{"id":"/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/n","name":"n","location":"l",
	  "sku":{"name":"Standard_LRS"},"tags":{"Kubernetes.io-Created-For-PVC-Name":"x"},
	  "properties":{"diskState":"Unattached","diskSizeGB":1}}]}`)
	disks, _, err := parseARMDisksPage(body)
	if err != nil || len(disks) != 1 {
		t.Fatalf("parse: %v %v", err, disks)
	}
	if disks[0].K8sPVCName != "x" {
		t.Errorf("tag com caixa diferente deveria casar: %+v", disks[0])
	}
}

func TestParseARMDisksPage_InvalidJSON(t *testing.T) {
	if _, _, err := parseARMDisksPage([]byte("not json")); err == nil {
		t.Error("esperava erro para JSON inválido")
	}
}

func TestResourceGroupFromID(t *testing.T) {
	cases := map[string]string{
		"/subscriptions/s/resourceGroups/MC_A_B_C/providers/x": "MC_A_B_C",
		"/subscriptions/s/resourcegroups/rg-low/providers/x":   "rg-low",
		"/subscriptions/s": "",
	}
	for id, want := range cases {
		if got := resourceGroupFromID(id); got != want {
			t.Errorf("resourceGroupFromID(%q) = %q, quer %q", id, got, want)
		}
	}
}

func TestListUnattachedDisks_RequiresSubscription(t *testing.T) {
	if _, err := ListUnattachedDisks(t.Context(), "  "); err == nil {
		t.Error("esperava erro sem subscription")
	}
}

func TestParseARMDisksPage_ProvisionedPerformance(t *testing.T) {
	// Ultra Disk / Premium SSD v2 expõem IOPS e throughput provisionados em properties.
	body := []byte(`{"value":[{"id":"/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/ultra","name":"ultra","location":"brazilsouth",
	  "sku":{"name":"UltraSSD_LRS"},"properties":{"diskState":"Unattached","diskSizeGB":1024,"diskIOPSReadWrite":5000,"diskMBpsReadWrite":200}}]}`)
	disks, _, err := parseARMDisksPage(body)
	if err != nil || len(disks) != 1 {
		t.Fatalf("parse: %v %v", err, disks)
	}
	if d := disks[0]; d.ProvisionedIOPS != 5000 || d.ProvisionedMBps != 200 || d.DiskType != "UltraSSD_LRS" {
		t.Errorf("performance provisionada não lida: %+v", d)
	}
}
