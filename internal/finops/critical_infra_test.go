package finops

import "testing"

// TestMatchCriticalInfraWorkloads_RealIncident reproduz o incidente real que motivou a Fase 1
// do plano de melhorias (FINOPS-IMPROVEMENTS-PLAN.md): o pool "ingress"
// (nginx-ingress-controller/velero/istio-ingressgateway) recebendo sugestão de downsize sem
// nenhum aviso de criticidade.
func TestMatchCriticalInfraWorkloads_RealIncident(t *testing.T) {
	names := []string{"nginx-ingress-controller", "velero", "istio-ingressgateway"}
	got := MatchCriticalInfraWorkloads(names)
	if len(got) != 3 {
		t.Fatalf("esperava os 3 workloads batendo, veio %v", got)
	}
}

func TestMatchCriticalInfraWorkloads_BusinessAppsNeverMatch(t *testing.T) {
	names := []string{"checkout-api", "order-request-api", "supply-forecast-service"}
	got := MatchCriticalInfraWorkloads(names)
	if len(got) != 0 {
		t.Fatalf("esperava zero matches (apps de negócio), veio %v", got)
	}
}

func TestMatchCriticalInfraWorkloads_CaseInsensitiveAndNoDuplicates(t *testing.T) {
	names := []string{"CoreDNS", "coredns", "checkout-api"}
	got := MatchCriticalInfraWorkloads(names)
	if len(got) != 2 {
		t.Fatalf("esperava 2 matches (CoreDNS + coredns, sem duplicar), veio %v", got)
	}
}

func TestMatchCriticalInfraWorkloads_EmptyInputReturnsEmptyNotNil(t *testing.T) {
	got := MatchCriticalInfraWorkloads(nil)
	if got == nil {
		t.Fatal("esperava slice vazio não-nil (evita 'null' no JSON), veio nil")
	}
	if len(got) != 0 {
		t.Fatalf("esperava vazio, veio %v", got)
	}
}

func TestMarkInsufficientForLargestWorkload_FlagsUndersizedAlternative(t *testing.T) {
	alts := []VMAlternative{
		{VMSize: "Standard_F2s_v2", CPUCores: 2, MemoryGB: 4},  // 2000m / 4096Mi de capacidade
		{VMSize: "Standard_F8s_v2", CPUCores: 8, MemoryGB: 16}, // 8000m / 16384Mi
	}
	// Maior pod do pool pede 3000m/6000Mi — com SafetyMargin=1.20, precisa de 3600m/7200Mi de
	// capacidade por node. A F2s_v2 (2000m/4096Mi) não comporta; a F8s_v2 comporta.
	MarkInsufficientForLargestWorkload(alts, 3000, 6000)

	if !alts[0].InsufficientForLargestWorkload {
		t.Error("esperava Standard_F2s_v2 marcada como insuficiente (2000m < 3000m*1.20)")
	}
	if alts[1].InsufficientForLargestWorkload {
		t.Error("esperava Standard_F8s_v2 NÃO marcada (8000m e 16384Mi comportam o maior pod)")
	}
}

func TestMarkInsufficientForLargestWorkload_NoDataNeverMarks(t *testing.T) {
	alts := []VMAlternative{{VMSize: "Standard_F2s_v2", CPUCores: 2, MemoryGB: 4}}
	MarkInsufficientForLargestWorkload(alts, 0, 0)
	if alts[0].InsufficientForLargestWorkload {
		t.Error("sem dado de maior request (0,0), nunca deveria marcar — nunca alarmar sem evidência")
	}
}

func TestMarkInsufficientForLargestWorkload_MemoryOnlyBottleneck(t *testing.T) {
	// CPU comporta, mas memória do maior pod não — deve marcar mesmo assim (qualquer uma das
	// duas dimensões insuficiente já é motivo de aviso).
	alts := []VMAlternative{{VMSize: "Standard_F8s_v2", CPUCores: 8, MemoryGB: 4}}
	MarkInsufficientForLargestWorkload(alts, 1000, 3800) // 3800*1.20 = 4560Mi > 4096Mi de capacidade
	if !alts[0].InsufficientForLargestWorkload {
		t.Error("esperava marcada por insuficiência de memória, mesmo com CPU sobrando")
	}
}
