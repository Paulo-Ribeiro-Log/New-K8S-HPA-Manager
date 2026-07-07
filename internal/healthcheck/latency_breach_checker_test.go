package healthcheck

import (
	"context"
	"sync"
	"testing"

	"k8s-hpa-manager/internal/monitoring/latencylookup"
)

func TestEnrichWithLatencyBreach_EscalatesOnlyWhenBothBreachAndDTProblem(t *testing.T) {
	items := []CorrelatedHealthItem{
		{
			WorkloadName:  "com-dt-problem-e-breach",
			Namespace:     "ns1",
			DTProblems:    []DynatraceHealth{{ProblemID: "P1"}},
			FinalSeverity: SeverityMedium,
		},
		{
			WorkloadName:  "com-dt-problem-sem-breach",
			Namespace:     "ns1",
			DTProblems:    []DynatraceHealth{{ProblemID: "P2"}},
			FinalSeverity: SeverityMedium,
		},
		{
			WorkloadName:  "sem-dt-problem",
			Namespace:     "ns1",
			FinalSeverity: SeverityMedium,
		},
	}

	var fetchCallsMu sync.Mutex
	fetchCalls := map[string]int{}
	fake := func(_ context.Context, dtURL, dtToken, cluster, namespace, workload string) latencylookup.Result {
		fetchCallsMu.Lock()
		fetchCalls[workload]++
		fetchCallsMu.Unlock()
		switch workload {
		case "com-dt-problem-e-breach":
			return latencylookup.Result{P95Ms: 800, P99Ms: 900, Source: "prometheus"}
		case "com-dt-problem-sem-breach":
			return latencylookup.Result{P95Ms: 50, P99Ms: 60, Source: "prometheus"}
		default:
			t.Fatalf("fetch não deveria ser chamado pra %q (sem DTProblems)", workload)
			return latencylookup.Result{}
		}
	}

	result := enrichWithLatencyBreach(context.Background(), items, "cluster1", "https://dt", "token", fake)

	if fetchCalls["sem-dt-problem"] != 0 {
		t.Errorf("fetch não deveria ter sido chamado pro workload sem DTProblems")
	}
	if fetchCalls["com-dt-problem-e-breach"] != 1 || fetchCalls["com-dt-problem-sem-breach"] != 1 {
		t.Errorf("esperado 1 chamada de fetch por workload com DTProblems, obtido: %+v", fetchCalls)
	}

	byName := map[string]CorrelatedHealthItem{}
	for _, item := range result {
		byName[item.WorkloadName] = item
	}

	breached := byName["com-dt-problem-e-breach"]
	if !breached.LatencyBreach {
		t.Errorf("esperado LatencyBreach=true pra P95=800ms (> threshold 500ms)")
	}
	if breached.FinalSeverity != SeverityCritical {
		t.Errorf("esperado FinalSeverity escalado pra Critical, obtido %q", breached.FinalSeverity)
	}
	if breached.LatencySource != "prometheus" {
		t.Errorf("esperado LatencySource=prometheus, obtido %q", breached.LatencySource)
	}

	notBreached := byName["com-dt-problem-sem-breach"]
	if notBreached.LatencyBreach {
		t.Errorf("esperado LatencyBreach=false pra P95=50ms (< threshold 500ms)")
	}
	if notBreached.FinalSeverity != SeverityMedium {
		t.Errorf("esperado FinalSeverity inalterado (Medium) sem breach, obtido %q", notBreached.FinalSeverity)
	}

	untouched := byName["sem-dt-problem"]
	if untouched.LatencySource != "" || untouched.LatencyBreach {
		t.Errorf("workload sem DTProblems não deveria ter sido enriquecido: %+v", untouched)
	}
}

func TestEnrichWithLatencyBreach_NoDTProblemsSkipsFetchEntirely(t *testing.T) {
	items := []CorrelatedHealthItem{
		{WorkloadName: "a", Namespace: "ns1", FinalSeverity: SeverityMedium},
		{WorkloadName: "b", Namespace: "ns1", FinalSeverity: SeverityLow},
	}
	called := false
	fake := func(_ context.Context, dtURL, dtToken, cluster, namespace, workload string) latencylookup.Result {
		called = true
		return latencylookup.Result{}
	}
	enrichWithLatencyBreach(context.Background(), items, "cluster1", "https://dt", "token", fake)
	if called {
		t.Errorf("fetch não deveria ser chamado quando nenhum item tem DTProblems")
	}
}

func TestEnrichWithLatencyBreach_SourceEmptyLeavesItemUntouched(t *testing.T) {
	items := []CorrelatedHealthItem{
		{
			WorkloadName:  "sem-dado",
			Namespace:     "ns1",
			DTProblems:    []DynatraceHealth{{ProblemID: "P1"}},
			FinalSeverity: SeverityHigh,
		},
	}
	fake := func(_ context.Context, dtURL, dtToken, cluster, namespace, workload string) latencylookup.Result {
		return latencylookup.Result{} // Source vazio = sem dado (nem DT nem Prometheus responderam)
	}
	result := enrichWithLatencyBreach(context.Background(), items, "cluster1", "https://dt", "token", fake)
	if result[0].LatencyBreach || result[0].LatencySource != "" || result[0].FinalSeverity != SeverityHigh {
		t.Errorf("item sem dado de latência não deveria ser alterado: %+v", result[0])
	}
}
