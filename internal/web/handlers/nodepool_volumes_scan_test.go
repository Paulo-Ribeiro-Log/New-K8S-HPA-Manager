package handlers

import (
	"testing"
	"time"
)

func ptr(n int) *int { return &n }

func TestBuildScanResult(t *testing.T) {
	data := &volumesData{
		Summary: VolumesSummary{SnapshotsKnown: true},
		Items: []VolumeItem{
			{Kind: "pvc", Namespace: "a", PVC: "cheio", PVCStatus: "Bound", UsageClass: "critical", UsagePercent: 93, DaysToFull: ptr(4)},
			{Kind: "pvc", Namespace: "a", PVC: "velho", PVCStatus: "Bound", Idle: true, AgeDays: ptr(90), IdleDays: ptr(12)},
			{Kind: "pvc", Namespace: "a", PVC: "novo-ocioso", PVCStatus: "Bound", Idle: true, AgeDays: ptr(3)},
			{Kind: "pvc", Namespace: "a", PVC: "semsnap", PVCStatus: "Bound"},
			{Kind: "pvc", Namespace: "a", PVC: "comsnap", PVCStatus: "Bound", Snapshots: 2},
			{Kind: "pv", PV: "orfao", Idle: true, AgeDays: ptr(40)},
		},
	}
	now := time.Now()
	r := buildScanResult("c1", data, nil, now)
	if r.Counts.Critical != 1 || r.Counts.IdleOld != 2 || r.Counts.NoSnapshot != 1 {
		t.Fatalf("counts = %+v", r.Counts)
	}
	if r.Findings[0].Kind != "critical" || r.Findings[len(r.Findings)-1].Kind != "no_snapshot" {
		t.Fatalf("ordem = %+v", r.Findings)
	}
	if r.NewCount != 0 {
		t.Fatalf("primeira varredura não marca novos: %d", r.NewCount)
	}
	// segunda varredura: surge um volume crítico e um "sem snapshot" novos
	data.Items = append(data.Items,
		VolumeItem{Kind: "pvc", Namespace: "b", PVC: "novo-cheio", PVCStatus: "Bound", UsageClass: "critical", UsagePercent: 95},
		VolumeItem{Kind: "pvc", Namespace: "b", PVC: "novo-semsnap", PVCStatus: "Bound"})
	r2 := buildScanResult("c1", data, r, now.Add(time.Hour))
	if r2.NewCount != 1 {
		t.Fatalf("new_count = %d (sem snapshot novo não conta)", r2.NewCount)
	}
	var newKinds int
	for _, f := range r2.Findings {
		if f.New {
			newKinds++
		}
	}
	if newKinds != 2 {
		t.Fatalf("achados marcados como novos = %d", newKinds)
	}
}

func TestBuildScanResultSemSnapshotAPI(t *testing.T) {
	data := &volumesData{Items: []VolumeItem{{Kind: "pvc", Namespace: "a", PVC: "x", PVCStatus: "Bound"}}}
	if r := buildScanResult("c", data, nil, time.Now()); r.Counts.NoSnapshot != 0 {
		t.Fatalf("sem API de snapshot não dá para afirmar 'sem snapshot': %+v", r.Counts)
	}
}
