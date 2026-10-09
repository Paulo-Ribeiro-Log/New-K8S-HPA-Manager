package finops

import (
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func intp(v int) *int { return &v }

func memHPA(name string, min, max, current, target int, currentUtil *int) DeepHPA {
	return DeepHPA{Name: name, Min: min, Max: max, Current: current, Metrics: []DeepHPAMetric{
		{Type: "Resource", Name: "memory", TargetType: "Utilization", TargetUtilization: target, CurrentUtilization: currentUtil},
	}}
}

// Caso real frete-aggregate-sku-mktp: HPA por memória com alvo 70%, preso no máximo (6/6), uso ~694 Mi
// com request de 477 Mi. O recomendado puro (P95 × 1,2) deixaria a utilização em ~82% → scale-up.
func TestApplyHPAAjustaRecomendadoAoAlvoDeMemoria(t *testing.T) {
	r := DeepWorkloadRow{Workload: "frete-aggregate-sku-mktp", CPUReqMillis: 500, MemReqMi: 477, UsageBasis: "p95",
		CPURecMillis: 60, MemRecMi: 848, MemLimitRecMi: 900}
	applyHPA(&r, memHPA("agg", 4, 6, 6, 70, nil), DeepHistory{}, 38, 694)

	if r.HPA == nil || r.HPA.State != "pinned_max" {
		t.Fatalf("estado do HPA = %+v; quer pinned_max", r.HPA)
	}
	if len(r.HPA.Projections) != 1 {
		t.Fatalf("projeções = %+v", r.HPA.Projections)
	}
	p := r.HPA.Projections[0]
	if !p.WouldTriggerScaleUp || !p.AdjustedForHPA || p.PlainProjectedPct < 80 || p.ProjectedPct > 70 {
		t.Errorf("projeção = %+v; quer scale-up com o puro e ≤70%% depois do ajuste", p)
	}
	if r.MemRecMi != 992 { // 694 / 0,70 = 991,4 → múltiplo de 16 acima
		t.Errorf("memória recomendada = %v; quer 992", r.MemRecMi)
	}
	if r.MemLimitRecMi < r.MemRecMi {
		t.Errorf("limit recomendado (%v) abaixo do request recomendado (%v)", r.MemLimitRecMi, r.MemRecMi)
	}
	for _, f := range []string{"hpa_pinned_max", "hpa_memory_metric", "rec_adjusted_for_hpa"} {
		if !containsStr(r.Flags, f) {
			t.Errorf("faltou a flag %s: %v", f, r.Flags)
		}
	}
}

// Caso real frete-hub: HPA por memória, alvo 85%, status 79% com request de 950 Mi. O recomendado
// (960 Mi) mantém a utilização em ~78%: nada a ajustar.
func TestApplyHPAUsaUtilizacaoDoStatus(t *testing.T) {
	r := DeepWorkloadRow{Workload: "frete-hub", CPUReqMillis: 800, MemReqMi: 950, UsageBasis: "p95", CPURecMillis: 80, MemRecMi: 960}
	applyHPA(&r, memHPA("frete-hub", 70, 120, 70, 85, intp(79)), DeepHistory{}, 35, 0)

	p := r.HPA.Projections[0]
	if p.CurrentPct != 79 || p.WouldTriggerScaleUp || p.AdjustedForHPA || p.ProjectedPct > 80 {
		t.Errorf("projeção = %+v", p)
	}
	if r.MemRecMi != 960 || containsStr(r.Flags, "rec_adjusted_for_hpa") {
		t.Errorf("não deveria ajustar: mem rec %v, flags %v", r.MemRecMi, r.Flags)
	}
	if r.HPA.State != "scaling" { // 79% está a menos de 10% do alvo: não é "preso no mínimo"
		t.Errorf("estado = %s; quer scaling", r.HPA.State)
	}
}

func TestHPAState(t *testing.T) {
	cases := []struct {
		h    DeepHPA
		hist DeepHistory
		want string
	}{
		{DeepHPA{Min: 1, Max: 1, Current: 1}, DeepHistory{}, "fixed"},
		{DeepHPA{Min: 2, Max: 5, Current: 5}, DeepHistory{}, "pinned_max"},
		{DeepHPA{Min: 70, Max: 100, Current: 70, Metrics: []DeepHPAMetric{{Type: "Resource", Name: "cpu", TargetType: "Utilization", TargetUtilization: 70, CurrentUtilization: intp(14)}}}, DeepHistory{}, "pinned_min"},
		{DeepHPA{Min: 3, Max: 10, Current: 3}, DeepHistory{HPANeverScaled: true}, "pinned_min"},
		{DeepHPA{Min: 3, Max: 10, Current: 5}, DeepHistory{}, "scaling"},
	}
	for i, c := range cases {
		if got := hpaState(c.h, c.hist); got != c.want {
			t.Errorf("caso %d: %s; quer %s", i, got, c.want)
		}
	}
}

func TestDeepHPAFrom(t *testing.T) {
	min := int32(70)
	target, current := int32(85), int32(79)
	avg := resource.MustParse("100")
	h := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Namespace: "fretes", Name: "frete-hub"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "frete-hub"},
			MinReplicas:    &min, MaxReplicas: 120,
			Metrics: []autoscalingv2.MetricSpec{
				{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceMemory, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType, AverageUtilization: &target}}},
				{Type: autoscalingv2.PodsMetricSourceType, Pods: &autoscalingv2.PodsMetricSource{
					Metric: autoscalingv2.MetricIdentifier{Name: "http_requests"}, Target: autoscalingv2.MetricTarget{Type: autoscalingv2.AverageValueMetricType, AverageValue: &avg}}},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 70,
			CurrentMetrics: []autoscalingv2.MetricStatus{{Type: autoscalingv2.ResourceMetricSourceType, Resource: &autoscalingv2.ResourceMetricStatus{
				Name: corev1.ResourceMemory, Current: autoscalingv2.MetricValueStatus{AverageUtilization: &current}}}},
		},
	}
	d := deepHPAFrom(h)
	if d.Min != 70 || d.Max != 120 || d.Current != 70 || len(d.Metrics) != 2 {
		t.Fatalf("HPA = %+v", d)
	}
	m := d.Metrics[0]
	if m.Name != "memory" || m.TargetType != "Utilization" || m.TargetUtilization != 85 || m.CurrentUtilization == nil || *m.CurrentUtilization != 79 {
		t.Errorf("métrica de memória = %+v", m)
	}
	if p := d.Metrics[1]; p.Type != "Pods" || p.Name != "http_requests" || p.TargetValue != "100" || p.CurrentUtilization != nil {
		t.Errorf("métrica Pods = %+v", p)
	}
}

func TestDeepThrottleQueries(t *testing.T) {
	p95, cur := deepThrottleQueries([]string{"calculo-de-fretes-prd", "logging"}, 7)
	for _, want := range []string{`namespace=~"calculo-de-fretes-prd|logging"`, "container_cpu_cfs_throttled_periods_total", "container_cpu_cfs_periods_total", "[7d:5m]", "quantile_over_time(0.95"} {
		if !strings.Contains(p95, want) {
			t.Errorf("query P95 sem %q: %s", want, p95)
		}
	}
	if strings.Contains(cur, "quantile_over_time") || !strings.HasSuffix(cur, "* 100") {
		t.Errorf("query atual inesperada: %s", cur)
	}
}

func TestBuildPoolDeepAnalysisComHPAEThrottling(t *testing.T) {
	in := fixtureCalculofrete(true, true)
	in.HPAs = map[string]DeepHPA{
		"calculo-de-fretes-prd/frete-aggregate-sku-mktp": memHPA("agg", 4, 6, 6, 70, nil),
		"calculo-de-fretes-prd/frete-b2c": {Name: "frete-b2c", Min: 20, Max: 100, Current: 20, Metrics: []DeepHPAMetric{
			{Type: "Resource", Name: "cpu", TargetType: "Utilization", TargetUtilization: 70, CurrentUtilization: intp(14)}}},
	}
	in.Throttling = map[string]DeepThrottle{}
	for _, p := range in.Pods {
		if p.Workload == "frete-b2c" {
			in.Throttling[p.Namespace+"/"+p.Name] = DeepThrottle{P95Pct: 22, CurrentPct: 3}
		}
	}
	in.ThrottleWindowDays = 7

	a := BuildPoolDeepAnalysis(in)
	codes := findingCodes(a.Findings)
	for _, c := range []string{"hpa_memory_pinned_max", "hpa_request_interaction", "hpa_pinned_min", "cpu_throttling"} {
		if _, ok := codes[c]; !ok {
			t.Errorf("faltou o achado %q", c)
		}
	}
	for _, w := range a.Workloads {
		switch w.Workload {
		case "frete-b2c":
			if !w.HasThrottle || w.ThrottleP95Pct != 22 || w.CPULimitRecMillis != 1800 || !containsStr(w.Flags, "cpu_throttled") {
				t.Errorf("frete-b2c: throttle=%v/%v limit rec=%v flags=%v", w.HasThrottle, w.ThrottleP95Pct, w.CPULimitRecMillis, w.Flags)
			}
		case "frete-aggregate-sku-mktp":
			if w.MemRecMi != 992 {
				t.Errorf("aggregate: memória recomendada = %v; quer 992 (ajustada ao HPA)", w.MemRecMi)
			}
		case "frete-hub":
			if w.HasThrottle || w.HPA != nil {
				t.Errorf("frete-hub não tem HPA nem throttling na fixture: %+v", w)
			}
		}
	}
	if a.ThrottleWindowDays != 7 {
		t.Errorf("janela de throttling = %d", a.ThrottleWindowDays)
	}
}

// Com min = max o HPA nunca escala: nada de ajustar o request recomendado ao alvo dele.
func TestApplyHPAFixoNaoAjusta(t *testing.T) {
	r := DeepWorkloadRow{Workload: "freight-bff", MemReqMi: 1500, UsageBasis: "p95", CPURecMillis: 50, MemRecMi: 448}
	applyHPA(&r, memHPA("freight-bff", 1, 1, 1, 70, intp(23)), DeepHistory{}, 5, 345)
	if r.HPA.State != "fixed" || r.MemRecMi != 448 || len(r.HPA.Projections) != 0 || containsStr(r.Flags, "rec_adjusted_for_hpa") {
		t.Errorf("HPA fixo: estado=%s mem rec=%v projeções=%v flags=%v", r.HPA.State, r.MemRecMi, r.HPA.Projections, r.Flags)
	}
}
