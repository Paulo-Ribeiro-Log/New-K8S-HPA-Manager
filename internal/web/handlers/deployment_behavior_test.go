package handlers

import (
	"testing"
	"time"

	dtclient "k8s-hpa-manager/internal/dynatrace"
	promclient "k8s-hpa-manager/internal/monitoring/client"
)

func TestPointsFromSeriesMap_MergesAndSorts(t *testing.T) {
	series := map[string]map[int64]float64{
		"replicas_desired": {3000: 3, 1000: 2, 2000: 3},
		"cpu":              {1000: 45.5, 2000: 50},
		"network_in":       {1000: 1024, 2000: 2048},
		"network_out":      {1000: 512},
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
	if points[0].NetworkInBytesSec != 1024 || points[1].NetworkInBytesSec != 2048 {
		t.Errorf("network_in incorreto: %+v", points)
	}
	if points[0].NetworkOutBytesSec != 512 || points[2].NetworkOutBytesSec != 0 {
		t.Errorf("network_out incorreto (ts=3000 ausente deveria ser 0): %+v", points)
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

// TestDynatraceSeriesToPointMap_MapsDesiredReplicas cobre a correção real: k8sWorkloadMetricDefs
// (metrics.go) trocou "pods_running"/"pods_ready_pct"/"pod_restarts" (metricId inexistentes ou com
// entidade primária errada pra CLOUD_APPLICATION — nunca retornavam dado real) por "pods_desired"
// (confirmado contra um tenant real). dynatraceSeriesToPointMap precisa mapear a chave NOVA pra
// replicas_desired — as antigas não existem mais na saída de GetDeploymentBehaviorMetrics.
func TestDynatraceSeriesToPointMap_MapsDesiredReplicas(t *testing.T) {
	series := []dtclient.MetricSeriesData{
		{Key: "pods_desired", Points: []dtclient.MetricPoint{{T: 1000, V: 4}}},
		{Key: "cpu_milli", Points: []dtclient.MetricPoint{{T: 1000, V: 250}}},
		{Key: "memory_mb", Points: []dtclient.MetricPoint{{T: 1000, V: 512}}},
	}

	out := dynatraceSeriesToPointMap(series)
	if out["replicas_desired"][1000] != 4 {
		t.Errorf("replicas_desired incorreto: %+v", out["replicas_desired"])
	}
	if _, ok := out["replicas_current"]; ok {
		t.Errorf("replicas_current não deveria estar presente (sem selector confirmado pra 'rodando agora')")
	}
	// cpu_milli/memory_mb são valores ABSOLUTOS (sem fonte de request pra normalizar em %) — vão
	// pras chaves dedicadas cpu_absolute/memory_absolute, NUNCA pra "cpu"/"memory" (essas
	// alimentam CPUUsagePct/MemoryUsagePct, que a função não deve inventar).
	if _, ok := out["cpu"]; ok {
		t.Errorf("cpu não deveria estar presente na saída (sem fonte de request no fallback DT)")
	}
	if _, ok := out["memory"]; ok {
		t.Errorf("memory não deveria estar presente na saída (sem fonte de request no fallback DT)")
	}
	if out["cpu_absolute"][1000] != 250 {
		t.Errorf("cpu_absolute incorreto: %+v", out["cpu_absolute"])
	}
	if out["memory_absolute"][1000] != 512 {
		t.Errorf("memory_absolute incorreto: %+v", out["memory_absolute"])
	}
}

func TestDtProblemsToMarkers_OpenProblem_EndTsNil(t *testing.T) {
	start := time.UnixMilli(1700000000000)
	problems := []dtclient.Problem{
		{ProblemID: "P-1", Title: "CPU alto", SeverityLevel: "PERFORMANCE", StartTime: start, EndTime: nil},
	}

	markers := dtProblemsToMarkers(problems)
	if len(markers) != 1 {
		t.Fatalf("esperado 1 marker, got %d", len(markers))
	}
	m := markers[0]
	if m.ProblemID != "P-1" || m.Title != "CPU alto" || m.Severity != "PERFORMANCE" {
		t.Errorf("campos incorretos: %+v", m)
	}
	if m.StartTs != start.UnixMilli() {
		t.Errorf("StartTs incorreto: got %d, want %d", m.StartTs, start.UnixMilli())
	}
	if m.EndTs != nil {
		t.Errorf("EndTs deveria ser nil pra problem ainda OPEN, got %v", *m.EndTs)
	}
}

func TestDtProblemsToMarkers_ClosedProblem_EndTsSet(t *testing.T) {
	start := time.UnixMilli(1700000000000)
	end := start.Add(30 * time.Minute)
	problems := []dtclient.Problem{
		{ProblemID: "P-2", Title: "Memória", SeverityLevel: "RESOURCE_CONTENTION", StartTime: start, EndTime: &end},
	}

	markers := dtProblemsToMarkers(problems)
	if len(markers) != 1 {
		t.Fatalf("esperado 1 marker, got %d", len(markers))
	}
	if markers[0].EndTs == nil {
		t.Fatal("EndTs não deveria ser nil pra problem já fechado")
	}
	if *markers[0].EndTs != end.UnixMilli() {
		t.Errorf("EndTs incorreto: got %d, want %d", *markers[0].EndTs, end.UnixMilli())
	}
}

func TestDtProblemsToMarkers_EmptyInput(t *testing.T) {
	markers := dtProblemsToMarkers(nil)
	if markers == nil || len(markers) != 0 {
		t.Errorf("esperado slice vazio (não nil) pra input vazio, got %+v", markers)
	}
}
