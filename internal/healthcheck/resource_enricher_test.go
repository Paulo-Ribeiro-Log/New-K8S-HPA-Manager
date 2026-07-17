package healthcheck

import "testing"

// TestVerdictFromP95 cobre os mesmos limiares do FinOps (verdictFromPrometheus em
// internal/finops/prometheus_enricher.go) reimplementados aqui: P95 >= 95% do request = oom_risk;
// request > recomendado (P95 × 1.2) em mais de 15% = superprovisioned; senão ok.
func TestVerdictFromP95(t *testing.T) {
	tests := []struct {
		name            string
		cpuRequestMilli int64
		cpuP95Milli     float64
		memRequestBytes int64
		memP95Bytes     float64
		want            string
	}{
		{
			name: "sem request configurado - nada a comparar",
			want: "",
		},
		{
			name:            "cpu no limite do request - oom_risk",
			cpuRequestMilli: 1000,
			cpuP95Milli:     960, // 96% >= 95%
			want:            "oom_risk",
		},
		{
			name:            "memoria no limite do request - oom_risk",
			cpuRequestMilli: 1000,
			cpuP95Milli:     500,
			memRequestBytes: 1_000_000_000,
			memP95Bytes:     980_000_000, // 98% >= 95%
			want:            "oom_risk",
		},
		{
			name:            "uso bem abaixo do request - superprovisioned",
			cpuRequestMilli: 1000,
			cpuP95Milli:     200, // recomendado = 240, excesso = 760 > 150 (15% de 1000)
			want:            "superprovisioned",
		},
		{
			name:            "uso proximo do recomendado - ok",
			cpuRequestMilli: 1000,
			cpuP95Milli:     800, // recomendado = 960, excesso = 40 < 150
			want:            "ok",
		},
		{
			name:            "exatamente no limiar de 95% - ainda ok, nao oom_risk",
			cpuRequestMilli: 1000,
			cpuP95Milli:     940, // 94% < 95%
			want:            "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verdictFromP95(tt.cpuRequestMilli, tt.cpuP95Milli, tt.memRequestBytes, tt.memP95Bytes)
			if got != tt.want {
				t.Errorf("verdictFromP95() = %q, want %q", got, tt.want)
			}
		})
	}
}
