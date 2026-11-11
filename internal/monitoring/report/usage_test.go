package report

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

type fakeProm struct {
	byKey map[string][][]interface{}
	fail  map[string]bool
}

func (f *fakeProm) QueryRange(_ context.Context, query string, _, _ time.Time, _ time.Duration) (*promclient.QueryRangeResult, error) {
	key := ""
	switch {
	case strings.Contains(query, "container_cpu") && strings.Contains(query, "requests"):
		key = "cpu_req"
	case strings.Contains(query, "container_cpu"):
		key = "cpu_lim"
	case strings.Contains(query, "requests"):
		key = "mem_req"
	default:
		key = "mem_lim"
	}
	if f.fail[key] {
		return nil, errors.New("boom")
	}
	r := &promclient.QueryRangeResult{Status: "success"}
	if vals, ok := f.byKey[key]; ok {
		r.Data.Result = append(r.Data.Result, struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
		}{Values: vals})
	}
	return r, nil
}

func pts(vals ...string) [][]interface{} {
	var out [][]interface{}
	for i, v := range vals {
		out = append(out, []interface{}{float64(i), v})
	}
	return out
}

func TestPodSelectorNaoCasaDeploymentVizinho(t *testing.T) {
	sel := podSelector("frete-hub")
	re := regexp.MustCompile("^" + strings.TrimSuffix(strings.TrimPrefix(sel, `pod=~"`), `"`) + "$")
	for pod, want := range map[string]bool{
		"frete-hub-7d9f8b6c5d-x7k2p":              true,
		"frete-hub-regionalizado-5b6c7d8f9-abcde": false, // outro Deployment
		"frete-hub-7d9f8b6c5d":                    false,
	} {
		if re.MatchString(pod) != want {
			t.Errorf("pod %q: casou=%v, esperava %v", pod, !want, want)
		}
	}
}

func TestUsageQueries(t *testing.T) {
	q := usageQueries("calculo-de-fretes-prd", "frete.hub", 5*time.Minute)
	if len(q) != 4 {
		t.Fatalf("esperava 4 consultas: %v", q)
	}
	for k, v := range q {
		if !strings.Contains(v, `namespace="calculo-de-fretes-prd"`) || !strings.Contains(v, `frete\\.hub-`) || !strings.HasSuffix(v, "* 100") {
			t.Errorf("%s: %s", k, v)
		}
		// os dois lados da divisão precisam ser deduplicados (kube-state-metrics duplicado -> 422)
		if n := strings.Count(v, "max by (pod,container)"); n != 2 {
			t.Errorf("%s: esperava 2 `max by (pod,container)` (um por lado), veio %d: %s", k, n, v)
		}
		if strings.Contains(v, "group_left") {
			t.Errorf("%s: a divisão deve ser um-para-um, sem group_left: %s", k, v)
		}
	}
	if !strings.Contains(q["cpu_lim"], "kube_pod_container_resource_limits") || !strings.Contains(q["mem_req"], `resource="memory"`) {
		t.Errorf("métricas trocadas: %v", q)
	}
}

func TestCollectUsageEstatisticas(t *testing.T) {
	f := &fakeProm{byKey: map[string][][]interface{}{
		"cpu_req": pts("1.9", "9.0", "6.7", "NaN"),
		"mem_req": pts("47.5", "48.0", "47.8"),
		// sem série de limite (containers sem limit): CPULim/MemLim ficam sem dado
	}}
	w := Window{Start: now.Add(-6 * time.Hour), End: now}
	plan, _ := PlanFor(w, now, nil)
	u, err := CollectUsage(context.Background(), f, "calculo-de-fretes-prd", "frete-hub", w, plan)
	if err != nil {
		t.Fatal(err)
	}
	if u.CPUReq == nil || u.CPUReq.Max != 9.0 || u.CPUReq.Min != 1.9 || u.CPUReq.Avg < 5.8 || u.CPUReq.Avg > 5.9 { // NaN ignorado
		t.Errorf("CPU pct request: %+v", u.CPUReq)
	}
	if u.MemReq == nil || u.MemReq.Max != 48.0 || u.MemReq.Min != 47.5 {
		t.Errorf("Mem pct request: %+v", u.MemReq)
	}
	if u.CPULim != nil || u.MemLim != nil {
		t.Errorf("sem série de limite deve ficar sem dado, nunca zero: %+v %+v", u.CPULim, u.MemLim)
	}
}

func TestCollectUsageFalhaParcialETotal(t *testing.T) {
	w := Window{Start: now.Add(-time.Hour), End: now}
	plan, _ := PlanFor(w, now, nil)
	f := &fakeProm{byKey: map[string][][]interface{}{"cpu_req": pts("10"), "mem_req": pts("20")}, fail: map[string]bool{"mem_lim": true}}
	u, err := CollectUsage(context.Background(), f, "ns", "app", w, plan)
	if err != nil || u.CPUReq == nil || len(u.Notes) != 1 || !strings.HasPrefix(u.Notes[0], "mem_lim:") {
		t.Errorf("falha parcial: %+v %v", u, err)
	}
	all := &fakeProm{fail: map[string]bool{"cpu_req": true, "cpu_lim": true, "mem_req": true, "mem_lim": true}}
	if _, err := CollectUsage(context.Background(), all, "ns", "app", w, plan); err == nil {
		t.Errorf("todas falharam: esperava erro")
	}
	if _, err := CollectUsage(context.Background(), f, `ns"x`, "app", w, plan); err == nil {
		t.Errorf("nome inválido deveria dar erro")
	}
}
