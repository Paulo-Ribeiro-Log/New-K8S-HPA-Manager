package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	promclient "k8s-hpa-manager/internal/monitoring/client"
)

func TestVectorByPVC(t *testing.T) {
	var res promclient.QueryResult
	raw := `{"status":"success","data":{"resultType":"vector","result":[
	  {"metric":{"namespace":"app","persistentvolumeclaim":"a"},"value":[1,"100"]},
	  {"metric":{"namespace":"app","persistentvolumeclaim":"a"},"value":[1,"150"]},
	  {"metric":{"namespace":"app","persistentvolumeclaim":"b"},"value":[1,"NaN"]},
	  {"metric":{"namespace":"","persistentvolumeclaim":"c"},"value":[1,"5"]}]}}`
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	got := vectorByPVC(&res)
	if len(got) != 1 || got["app/a"] != 150 {
		t.Fatalf("esperava só app/a=150 (maior série, sem NaN nem série sem namespace), veio %v", got)
	}
}

func TestDaysToFull(t *testing.T) {
	gib := 1024.0 * 1024 * 1024
	d := daysToFull(VolumeUsage{UsedBytes: 90 * gib, CapacityBytes: 100 * gib, GrowthPerDay: 2 * gib, HasGrowth: true})
	if d == nil || *d != 5 {
		t.Fatalf("10 GiB livres a 2 GiB/dia = 5 dias, veio %v", d)
	}
	for name, u := range map[string]VolumeUsage{
		"sem histórico":      {UsedBytes: 1, CapacityBytes: 10 * gib, GrowthPerDay: 5 * gib},
		"ruído":              {UsedBytes: 1, CapacityBytes: 10 * gib, GrowthPerDay: 100, HasGrowth: true},
		"encolhendo":         {UsedBytes: 1, CapacityBytes: 10 * gib, GrowthPerDay: -gib, HasGrowth: true},
		"já cheio (sem cap)": {UsedBytes: 10 * gib, CapacityBytes: 10 * gib, GrowthPerDay: gib, HasGrowth: true},
	} {
		if daysToFull(u) != nil {
			t.Errorf("%s: não deveria prever", name)
		}
	}
}

func TestApplyVolumeUsage(t *testing.T) {
	gib := 1024.0 * 1024 * 1024
	items := []VolumeItem{
		{Kind: "pvc", Namespace: "n", PVC: "cheio", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "enchendo", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "atencao", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "folgado", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "pequeno", CapacityBytes: 5 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "ok", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pvc", Namespace: "n", PVC: "sem-metrica", CapacityBytes: 100 * gib, Issues: []string{}},
		{Kind: "pv", PV: "solto", Issues: []string{}},
	}
	usage := map[string]VolumeUsage{
		"n/cheio":    {UsedBytes: 95 * gib, CapacityBytes: 100 * gib},
		"n/enchendo": {UsedBytes: 50 * gib, CapacityBytes: 100 * gib, GrowthPerDay: 10 * gib, HasGrowth: true}, // 5 dias
		"n/atencao":  {UsedBytes: 40 * gib, CapacityBytes: 100 * gib, GrowthPerDay: 3 * gib, HasGrowth: true},  // 20 dias
		"n/folgado":  {UsedBytes: 5 * gib, CapacityBytes: 100 * gib},
		"n/pequeno":  {UsedBytes: 0.1 * gib, CapacityBytes: 5 * gib},
		"n/ok":       {UsedBytes: 50 * gib, CapacityBytes: 100 * gib, GrowthPerDay: 0.1 * gib, HasGrowth: true}, // ~500 dias
	}
	var sum VolumesSummary
	applyVolumeUsage(items, usage, &sum)
	recountIssues(items, &sum)

	want := map[string]string{"cheio": "critical", "enchendo": "critical", "atencao": "warning", "folgado": "oversized", "pequeno": "ok", "ok": "ok", "sem-metrica": ""}
	for _, it := range items {
		if it.Kind == "pvc" && it.UsageClass != want[it.PVC] {
			t.Errorf("%s: classe %q, esperava %q", it.PVC, it.UsageClass, want[it.PVC])
		}
	}
	if d := items[1].DaysToFull; d == nil || *d != 5 || !strings.Contains(strings.Join(items[1].Issues, "|"), "Enche em ~5 dia") {
		t.Fatalf("enchendo: %+v", items[1])
	}
	if sum.UsageKnown != 6 || sum.UsageCritical != 2 || sum.UsageWarning != 1 || sum.UsageOversized != 1 || sum.WithIssues != 4 {
		t.Fatalf("resumo: %+v", sum)
	}
	if items[0].UsagePercent != 95 {
		t.Fatalf("percentual: %v", items[0].UsagePercent)
	}
}
