package finops

import "testing"

func TestRecommendedLimits_CPUPreservesExistingRatio(t *testing.T) {
	// limit atual 400m / request atual 200m = razão 2.0 — novo request recomendado 100m ×
	// 2.0 = 200m de limit recomendado (preserva a folga de burst já tolerada).
	cpuLimitRec, _ := recommendedLimits(100, 0, 0, 0, 400, 200, 0)
	if cpuLimitRec != 200 {
		t.Errorf("cpuLimitRec = %v, want 200 (preserva razão 2.0)", cpuLimitRec)
	}
}

func TestRecommendedLimits_CPUFallsBackToDefaultRatioWithoutCurrentLimit(t *testing.T) {
	// sem limit configurado hoje (curCPULimit=0) → usa defaultCPULimitRatio (2×).
	cpuLimitRec, _ := recommendedLimits(150, 0, 0, 0, 0, 200, 0)
	want := 150 * defaultCPULimitRatio
	if cpuLimitRec != want {
		t.Errorf("cpuLimitRec = %v, want %v (proporção default)", cpuLimitRec, want)
	}
}

func TestRecommendedLimits_MemUsesMaxWhenHigherThanP95(t *testing.T) {
	// pico observado (300) > P95 (200) → usa o pico × 1.30, não o P95.
	_, memLimitRec := recommendedLimits(0, 250, 200, 300, 0, 0, 0)
	want := round2(300 * recommendedMemLimitMargin)
	if memLimitRec != want {
		t.Errorf("memLimitRec = %v, want %v (deveria usar o pico, maior que o P95)", memLimitRec, want)
	}
}

func TestRecommendedLimits_MemFallsBackToP95WhenNoMaxData(t *testing.T) {
	// memMax=0 (ex: Dynatrace sem métrica de pico) → cai pro P95 × margem.
	_, memLimitRec := recommendedLimits(0, 250, 200, 0, 0, 0, 0)
	want := round2(200 * recommendedMemLimitMargin)
	if memLimitRec != want {
		t.Errorf("memLimitRec = %v, want %v (fallback pro P95 sem dado de pico)", memLimitRec, want)
	}
}

func TestRecommendedLimits_ZeroRequestRecommendedYieldsZeroLimits(t *testing.T) {
	// sem request recomendado (workload sem uso real conhecido) — nunca inventa um limit.
	cpuLimitRec, memLimitRec := recommendedLimits(0, 0, 100, 150, 400, 200, 300)
	if cpuLimitRec != 0 || memLimitRec != 0 {
		t.Errorf("esperava (0,0) sem request recomendado, veio (%v,%v)", cpuLimitRec, memLimitRec)
	}
}
