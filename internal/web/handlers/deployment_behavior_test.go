package handlers

import (
	"testing"

	dtclient "k8s-hpa-manager/internal/dynatrace"
	promclient "k8s-hpa-manager/internal/monitoring/client"
)

func TestPointsFromSeriesMap_MergesAndSorts(t *testing.T) {
	series := map[string]map[int64]float64{
		"replicas_desired": {3000: 3, 1000: 2, 2000: 3},
		"cpu":              {1000: 45.5, 2000: 50},
		// "restarts" ausente de propósito — deve virar 0 em todos os pontos, sem panicar.
	}

	points := pointsFromSeriesMap(series)
	if len(points) != 3 {
		t.Fatalf("esperado 3 pontos, got %d", len(points))
	}
	if points[0].Timestamp != 1000 || points[1].Timestamp != 2000 || points[2].Timestamp != 3000 {
		t.Fatalf("pontos não ordenados por timestamp: %+v", points)
	}
	if points[0].ReplicasDesired != 2 || points[2].ReplicasDesired != 3 {
		t.Errorf("replicas_desired incorreto: %+v", points)
	}
	if points[2].CPUUsagePct != 0 {
		t.Errorf("cpu ausente no ts=3000 deveria ser 0, got %v", points[2].CPUUsagePct)
	}
	if points[0].Restarts != 0 {
		t.Errorf("série restarts ausente deveria produzir 0, got %v", points[0].Restarts)
	}
}

func TestDeriveScaleEvents_DetectsChangesOnly(t *testing.T) {
	points := []DeploymentBehaviorPoint{
		{Timestamp: 1000, ReplicasDesired: 3},
		{Timestamp: 2000, ReplicasDesired: 3}, // sem mudança — não deve gerar evento
		{Timestamp: 3000, ReplicasDesired: 5},
		{Timestamp: 4000, ReplicasDesired: 2},
	}
	events := deriveScaleEvents(points)
	if len(events) != 2 {
		t.Fatalf("esperado 2 scale events, got %d: %+v", len(events), events)
	}
	if events[0].Timestamp != 3000 || events[0].FromReplicas != 3 || events[0].ToReplicas != 5 {
		t.Errorf("primeiro evento incorreto: %+v", events[0])
	}
	if events[1].Timestamp != 4000 || events[1].FromReplicas != 5 || events[1].ToReplicas != 2 {
		t.Errorf("segundo evento incorreto: %+v", events[1])
	}
}

func TestDeriveScaleEvents_EmptyInput(t *testing.T) {
	events := deriveScaleEvents(nil)
	if events == nil || len(events) != 0 {
		t.Errorf("esperado slice vazio (não nil) pra input vazio, got %+v", events)
	}
}

func TestPrometheusSeriesToPointMap_ConvertsSecondsToMillis(t *testing.T) {
	raw := map[string]*promclient.QueryRangeResult{
		"cpu": {},
	}
	raw["cpu"].Data.ResultType = "matrix"
	raw["cpu"].Data.Result = []struct {
		Metric map[string]string `json:"metric"`
		Values [][]interface{}   `json:"values"`
	}{
		{Values: [][]interface{}{{float64(1700000000), "42.5"}}},
	}

	out := prometheusSeriesToPointMap(raw)
	m, ok := out["cpu"]
	if !ok {
		t.Fatalf("chave 'cpu' ausente no resultado")
	}
	val, ok := m[1700000000*1000]
	if !ok {
		t.Fatalf("timestamp em ms não encontrado — chaves: %+v", m)
	}
	if val != 42.5 {
		t.Errorf("valor incorreto: got %v, want 42.5", val)
	}
}

func TestDynatraceSeriesToPointMap_DerivesReadyReplicas(t *testing.T) {
	series := []dtclient.MetricSeriesData{
		{Key: "pods_running", Points: []dtclient.MetricPoint{{T: 1000, V: 4}}},
		{Key: "pods_ready_pct", Points: []dtclient.MetricPoint{{T: 1000, V: 75}}},
		{Key: "pod_restarts", Points: []dtclient.MetricPoint{{T: 1000, V: 2}}},
		// cpu_milli/memory_mb propositalmente presentes mas SEM equivalente de saída — a função
		// não deve inventar cpu/memory percentuais sem uma fonte de request (limitação documentada).
		{Key: "cpu_milli", Points: []dtclient.MetricPoint{{T: 1000, V: 250}}},
	}

	out := dynatraceSeriesToPointMap(series)
	if out["replicas_current"][1000] != 4 {
		t.Errorf("replicas_current incorreto: %+v", out["replicas_current"])
	}
	if out["replicas_ready"][1000] != 3 { // 4 * 75% = 3
		t.Errorf("replicas_ready incorreto: %+v", out["replicas_ready"])
	}
	if out["restarts"][1000] != 2 {
		t.Errorf("restarts incorreto: %+v", out["restarts"])
	}
	if _, ok := out["cpu"]; ok {
		t.Errorf("cpu não deveria estar presente na saída (sem fonte de request no fallback DT)")
	}
}
