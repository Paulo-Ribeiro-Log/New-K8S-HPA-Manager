package finops

import (
	"math"
	"testing"
)

// frete-hub (calculo-de-fretes-prd, cluster oferta-prd): 70 pods, request de 800m/950Mi por pod,
// uso P95 de ~51m/845Mi por pod. Antes da correção o request do FinOpsWorkload era a SOMA dos 70
// pods (56000m/66500Mi) e era comparado com o recomendado de UM pod (60,89m/1013,98Mi): o
// desperdício saía como 99,2% do custo (R$ 10.757,96 de R$ 10.846,55).
func freteHubRaw() rawWorkload {
	return rawWorkload{
		Namespace: "calculo-de-fretes-prd", Workload: "frete-hub", Pods: 70,
		CPURequestMillis: 56000, MemRequestMi: 66500, // somas dos 70 pods
		CPULimitMillis: 70000, MemLimitMi: 105000,
	}
}

func TestAllocateCosts_RequestsAreReportedPerPod(t *testing.T) {
	cap := clusterCapacity{CPUMillicores: 200000, MemoryMi: 400000}
	out := allocateCosts([]rawWorkload{freteHubRaw()}, cap, 2109.16, 5.1426)
	if len(out) != 1 {
		t.Fatalf("esperado 1 workload, got %d", len(out))
	}
	w := out[0]
	if w.CPURequestMillis != 800 || w.MemRequestMi != 950 {
		t.Errorf("request deveria ser POR POD (800m/950Mi), got %.2fm/%.2fMi", w.CPURequestMillis, w.MemRequestMi)
	}
	if w.CPULimitMillis != 1000 || w.MemLimitMi != 1500 {
		t.Errorf("limit deveria ser POR POD (1000m/1500Mi), got %.2fm/%.2fMi", w.CPULimitMillis, w.MemLimitMi)
	}
	// A fração de custo continua vindo do request TOTAL: (56000/200000 + 66500/400000)/2 = 0.223125.
	wantShareUSD := 0.223125 * 2109.16
	if math.Abs(w.CostShareUSD-wantShareUSD) > 0.05 {
		t.Errorf("custo alocado mudou (deveria usar o request total): got %.2f, want %.2f", w.CostShareUSD, wantShareUSD)
	}
}

func TestCalculateWaste_FreteHubNotInflatedByPodCount(t *testing.T) {
	cap := clusterCapacity{CPUMillicores: 200000, MemoryMi: 400000}
	w := allocateCosts([]rawWorkload{freteHubRaw()}, cap, 2109.16, 5.1426)[0]
	w.CostShareBRL = 10846.55
	w.CPURecommendedMillis = 60.89 // P95 (por pod) × 1,20
	w.MemRecommendedMi = 1013.98

	got := calculateWaste(&w)

	// CPU: (800-60,89)/800 = 92,4% de folga. Memória: recomendado (1014Mi) > request (950Mi) → 0.
	// waste = (0,9239 + 0) / 2 × 10.846,55 ≈ 5.010,6 — antes da correção: 10.757,96.
	if got > 5100 || got < 4900 {
		t.Errorf("waste deveria ficar ~R$ 5.010 (só a CPU sobra), got %.2f", got)
	}
	if got > 0.6*w.CostShareBRL {
		t.Errorf("waste (%.2f) não pode passar de metade do custo quando a memória está no limite", got)
	}
}

func TestVerdictFromPrometheus_MultiPodUsesPerPodRequest(t *testing.T) {
	cap := clusterCapacity{CPUMillicores: 200000, MemoryMi: 400000}

	// Pico de CPU por pod (780m) encostado no request por pod (800m): oom_risk. Com o request
	// TOTAL (56000m) o risco nunca disparava em workload multi-pod.
	risky := allocateCosts([]rawWorkload{freteHubRaw()}, cap, 2109.16, 5.1426)[0]
	risky.CPUP95Millis = 780
	risky.CPURecommendedMillis = 936
	if v := verdictFromPrometheus(&risky); v != "oom_risk" {
		t.Errorf("P95=780m contra request=800m/pod deveria ser oom_risk, got %q", v)
	}

	// P95 a ~88% do request por pod e recomendado (P95×1,2) acima do request: sem risco (< 95%) e
	// sem desperdício — antes da correção o request total (66500Mi) fazia a memória parecer 98%
	// ociosa e o workload virava "superprovisioned".
	w := allocateCosts([]rawWorkload{freteHubRaw()}, cap, 2109.16, 5.1426)[0]
	w.CPUP95Millis, w.CPURecommendedMillis = 700, 840
	w.MemP95Mi, w.MemRecommendedMi = 800, 960
	if v := verdictFromPrometheus(&w); v != "ok" {
		t.Errorf("workload dimensionado corretamente deveria ser ok, got %q", v)
	}
}

func TestAllocateCosts_SinglePodUnchanged(t *testing.T) {
	cap := clusterCapacity{CPUMillicores: 32000, MemoryMi: 131072}
	out := allocateCosts([]rawWorkload{{Namespace: "ns", Workload: "solo", Pods: 1, CPURequestMillis: 500, MemRequestMi: 512}}, cap, 1000, 5.2)
	if out[0].CPURequestMillis != 500 || out[0].MemRequestMi != 512 {
		t.Errorf("workload de 1 pod não pode mudar: got %.2f/%.2f", out[0].CPURequestMillis, out[0].MemRequestMi)
	}
}
