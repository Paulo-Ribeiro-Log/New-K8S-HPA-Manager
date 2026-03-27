package finops

import (
	"testing"

	"k8s-hpa-manager/internal/storage"
)

func TestStripHash(t *testing.T) {
	cases := map[string]string{
		"my-app-7d9f8b6c5":        "my-app",
		"my-app-deploy-abc12":     "my-app-deploy",
		"my-app":                  "my-app",
		"single":                  "single",
		"tms-order-acl-5f9d7c8b6": "tms-order-acl",
	}
	for input, want := range cases {
		got := stripHash(input)
		if got != want {
			t.Errorf("stripHash(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestShouldIncludeNamespace(t *testing.T) {
	// Sem filtro — exclui sistema
	if shouldIncludeNamespace("kube-system", nil) {
		t.Error("kube-system deveria ser excluído sem filtro")
	}
	if !shouldIncludeNamespace("my-app", nil) {
		t.Error("my-app deveria ser incluído sem filtro")
	}

	// Com filtro explícito
	filter := buildNamespaceFilter([]string{"ns-a", "ns-b"})
	if !shouldIncludeNamespace("ns-a", filter) {
		t.Error("ns-a deveria estar no filtro")
	}
	if shouldIncludeNamespace("kube-system", filter) {
		t.Error("kube-system não está no filtro explícito")
	}
	if shouldIncludeNamespace("ns-c", filter) {
		t.Error("ns-c não está no filtro")
	}
}

func TestCalculatePoolCosts(t *testing.T) {
	pricer, err := NewAzurePricer("brazilsouth")
	if err != nil {
		t.Fatalf("criar pricer: %v", err)
	}
	defer pricer.Close()

	exchange := NewExchangeRateProvider()
	calc := NewCalculator(pricer, exchange)
	rate, _ := exchange.Get()

	pools := []storage.NodePoolRegistryEntry{
		{Cluster: "test", NodePool: "system", VMSize: "Standard_D4s_v3", NodeCount: 2, Mode: "System"},
		{Cluster: "test", NodePool: "user",   VMSize: "Standard_D8s_v3", NodeCount: 3, Mode: "User"},
	}

	finOpsPools, cap, totalUSD, err := calc.calculatePoolCosts(pools, rate)
	if err != nil {
		t.Fatalf("calculatePoolCosts: %v", err)
	}

	if len(finOpsPools) != 2 {
		t.Errorf("esperado 2 pools, got %d", len(finOpsPools))
	}
	if cap.CPUMillicores == 0 || cap.MemoryMi == 0 {
		t.Errorf("capacidade zerada: cpu=%d mem=%d", cap.CPUMillicores, cap.MemoryMi)
	}
	if totalUSD <= 0 {
		t.Errorf("custo total inválido: %f", totalUSD)
	}

	// D4s_v3: 4 vCPU × 2 nodes = 8000 millicores
	// D8s_v3: 8 vCPU × 3 nodes = 24000 millicores
	// Total: 32000 millicores
	if cap.CPUMillicores != 32000 {
		t.Errorf("CPUMillicores esperado 32000, got %d", cap.CPUMillicores)
	}

	t.Logf("Pool costs: %.2f USD / R$ %.2f BRL/mês", totalUSD, totalUSD*rate)
	t.Logf("Capacidade: %d millicores CPU, %d MiB RAM", cap.CPUMillicores, cap.MemoryMi)
	for _, p := range finOpsPools {
		t.Logf("  %-12s %-20s %d nodes  $%.3f/h  R$%.2f/mês  [fonte=%s]",
			p.Name, p.VMSize, p.NodeCount, p.VMPriceUSDHour, p.MonthlyCostBRL, p.PriceSource)
	}
}

func TestAllocateCosts(t *testing.T) {
	rate := 5.20
	clusterCostUSD := 1000.0
	cap := clusterCapacity{CPUMillicores: 32000, MemoryMi: 131072}

	workloads := []rawWorkload{
		{Namespace: "ns-a", Workload: "app-a", Pods: 3, CPURequestMillis: 3000, MemRequestMi: 3072, HPAMin: 2, HPAMax: 10, HPACurrent: 3},
		{Namespace: "ns-a", Workload: "app-b", Pods: 1, CPURequestMillis: 500,  MemRequestMi: 512},
		{Namespace: "ns-b", Workload: "app-c", Pods: 2, CPURequestMillis: 0,    MemRequestMi: 0},
	}

	result := allocateCosts(workloads, cap, clusterCostUSD, rate)

	if len(result) != 3 {
		t.Errorf("esperado 3 workloads, got %d", len(result))
	}

	// app-a deve ter o maior custo
	if result[0].Workload != "app-a" {
		t.Errorf("app-a deveria ser o mais caro, got %s", result[0].Workload)
	}

	// app-c sem requests → no_request
	for _, wl := range result {
		if wl.Workload == "app-c" && wl.Verdict != "no_request" {
			t.Errorf("app-c deveria ter verdict no_request, got %s", wl.Verdict)
		}
	}

	// app-a tem HPA min=2, max=10, current=3 → ratio=0.3 < 0.35 → superprovisioned
	for _, wl := range result {
		if wl.Workload == "app-a" && wl.Verdict != "superprovisioned" {
			t.Errorf("app-a deveria ser superprovisioned, got %s", wl.Verdict)
		}
	}

	t.Logf("Workloads alocados:")
	for _, wl := range result {
		t.Logf("  %-10s  R$%.2f/mês  HPA[%d/%d/%d]  min=R$%.2f  max=R$%.2f  verdict=%s",
			wl.Workload, wl.CostShareBRL,
			wl.HPAMin, wl.HPACurrent, wl.HPAMax,
			wl.HPACostMinBRL, wl.HPACostMaxBRL, wl.Verdict)
	}
}

func TestAggregateNamespaces(t *testing.T) {
	workloads := []FinOpsWorkload{
		{Namespace: "ns-a", Workload: "w1", CostShareUSD: 100, CostShareBRL: 520},
		{Namespace: "ns-a", Workload: "w2", CostShareUSD: 50,  CostShareBRL: 260},
		{Namespace: "ns-b", Workload: "w3", CostShareUSD: 200, CostShareBRL: 1040},
	}

	namespaces := aggregateNamespaces(workloads)

	if len(namespaces) != 2 {
		t.Errorf("esperado 2 namespaces, got %d", len(namespaces))
	}
	// ns-b deve vir primeiro (maior custo)
	if namespaces[0].Namespace != "ns-b" {
		t.Errorf("ns-b deveria ser o mais caro, got %s", namespaces[0].Namespace)
	}
	if namespaces[0].MonthlyCostBRL != 1040 {
		t.Errorf("ns-b custo esperado 1040, got %.2f", namespaces[0].MonthlyCostBRL)
	}
	if namespaces[1].MonthlyCostBRL != 780 {
		t.Errorf("ns-a custo esperado 780 (520+260), got %.2f", namespaces[1].MonthlyCostBRL)
	}
}
