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
	calc := NewCalculator(pricer, nil, exchange)
	rate, _ := exchange.Get()

	pools := []storage.NodePoolRegistryEntry{
		{Cluster: "test", NodePool: "system", VMSize: "Standard_D4s_v3", NodeCount: 2, Mode: "System"},
		{Cluster: "test", NodePool: "user", VMSize: "Standard_D8s_v3", NodeCount: 3, Mode: "User"},
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
		{Namespace: "ns-a", Workload: "app-b", Pods: 1, CPURequestMillis: 500, MemRequestMi: 512},
		{Namespace: "ns-b", Workload: "app-c", Pods: 2, CPURequestMillis: 0, MemRequestMi: 0},
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
		{Namespace: "ns-a", Workload: "w2", CostShareUSD: 50, CostShareBRL: 260},
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

// TestBuildSummary_MetricsWorkloadsEnriched cobre o bug real relatado pelo usuário: um scan onde
// TODOS os workloads/pools vieram com desperdício R$0, CPU/Mem 0%, "Com Oportunidade 0" — sinal
// indistinguível, sem esse campo, de "cluster genuinamente sem desperdício algum" (quando na
// real era falha silenciosa de coleta Prometheus/Dynatrace). Confirma que MetricsWorkloadsEnriched
// conta corretamente os workloads com MetricsSource preenchido (por qualquer fonte).
func TestBuildSummary_MetricsWorkloadsEnriched(t *testing.T) {
	workloads := []FinOpsWorkload{
		{Namespace: "ns-a", Workload: "w1", MetricsSource: "prometheus"},
		{Namespace: "ns-a", Workload: "w2", MetricsSource: "dynatrace"},
		{Namespace: "ns-b", Workload: "w3", MetricsSource: ""}, // sem enriquecimento
	}

	summary := buildSummary(workloads, nil, 0, 5.2)
	if summary.MetricsWorkloadsEnriched != 2 {
		t.Errorf("esperava MetricsWorkloadsEnriched=2, got %d", summary.MetricsWorkloadsEnriched)
	}
	if summary.WorkloadsAnalyzed != 3 {
		t.Errorf("esperava WorkloadsAnalyzed=3, got %d", summary.WorkloadsAnalyzed)
	}
}

// TestBuildSummary_MetricsWorkloadsEnriched_AllEmpty cobre o cenário exato do bug real — nenhum
// workload recebeu enriquecimento (o sinal de falha silenciosa que BuildReport usa junto de
// MetricsAttempted pra decidir se avisa o usuário).
func TestBuildSummary_MetricsWorkloadsEnriched_AllEmpty(t *testing.T) {
	workloads := []FinOpsWorkload{
		{Namespace: "ns-a", Workload: "w1"},
		{Namespace: "ns-a", Workload: "w2"},
	}
	summary := buildSummary(workloads, nil, 0, 5.2)
	if summary.MetricsWorkloadsEnriched != 0 {
		t.Errorf("esperava MetricsWorkloadsEnriched=0, got %d", summary.MetricsWorkloadsEnriched)
	}
}

// TestReclassifyNoDataVerdicts cobre o bug real corrigido (FINOPS-IMPROVEMENTS-PLAN.md F0.2):
// determineVerdict (baseado só em HPA, roda ANTES de qualquer enriquecimento) devolve "ok" tanto
// pro workload genuinamente sem HPA quanto pro workload que nunca recebeu enriquecimento nenhum
// — as duas situações ficavam indistinguíveis, badge verde "Eficiente" idêntico pros dois casos.
func TestReclassifyNoDataVerdicts(t *testing.T) {
	workloads := []FinOpsWorkload{
		// Sem MetricsSource E verdict "ok" — o caso do bug: deve virar "sem_dados".
		{Namespace: "ns-a", Workload: "sem-enriquecimento", Verdict: "ok", MetricsSource: ""},
		// Enriquecido (Prometheus) com verdict "ok" real — verificado e saudável, não deve mudar.
		{Namespace: "ns-a", Workload: "verificado-ok", Verdict: "ok", MetricsSource: "prometheus"},
		// Sem MetricsSource mas verdict "no_request" — conclusão já válida só com dado de HPA/
		// request, não depende de métrica de uso nenhuma; não deve virar "sem_dados".
		{Namespace: "ns-b", Workload: "sem-request", Verdict: "no_request", MetricsSource: ""},
		// Sem MetricsSource mas verdict "superprovisioned" — idem, conclusão via HPA, preservar.
		{Namespace: "ns-b", Workload: "hpa-superprovisionado", Verdict: "superprovisioned", MetricsSource: ""},
	}

	reclassifyNoDataVerdicts(workloads)

	if workloads[0].Verdict != "sem_dados" {
		t.Errorf("workload sem MetricsSource e verdict 'ok' deveria virar 'sem_dados', ficou %q", workloads[0].Verdict)
	}
	if workloads[1].Verdict != "ok" {
		t.Errorf("workload verificado via Prometheus deveria continuar 'ok', ficou %q", workloads[1].Verdict)
	}
	if workloads[2].Verdict != "no_request" {
		t.Errorf("verdict 'no_request' (conclusão via HPA/request, não uso) não deveria ser tocado, ficou %q", workloads[2].Verdict)
	}
	if workloads[3].Verdict != "superprovisioned" {
		t.Errorf("verdict 'superprovisioned' (conclusão via HPA) não deveria ser tocado, ficou %q", workloads[3].Verdict)
	}
}

// TestBuildSummary_NoDataCount confirma que buildSummary conta corretamente o novo verdict
// "sem_dados" num campo próprio (NoDataCount) — nunca misturado com SuperprovisionedCount/
// OOMRiskCount (que representariam desperdício/risco CONFIRMADOS, não desconhecidos).
func TestBuildSummary_NoDataCount(t *testing.T) {
	workloads := []FinOpsWorkload{
		{Namespace: "ns-a", Workload: "w1", Verdict: "sem_dados"},
		{Namespace: "ns-a", Workload: "w2", Verdict: "sem_dados"},
		{Namespace: "ns-a", Workload: "w3", Verdict: "ok", MetricsSource: "prometheus"},
	}
	summary := buildSummary(workloads, nil, 0, 5.2)
	if summary.NoDataCount != 2 {
		t.Errorf("esperava NoDataCount=2, got %d", summary.NoDataCount)
	}
	if summary.SuperprovisionedCount != 0 || summary.OOMRiskCount != 0 {
		t.Errorf("sem_dados não deveria contar como desperdício/risco — got superprovisioned=%d oom_risk=%d", summary.SuperprovisionedCount, summary.OOMRiskCount)
	}
}
