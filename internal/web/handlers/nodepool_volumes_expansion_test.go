package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplyExpansion(t *testing.T) {
	pvcs := []corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "x"}, Status: corev1.PersistentVolumeClaimStatus{Conditions: []corev1.PersistentVolumeClaimCondition{
			{Type: corev1.PersistentVolumeClaimFileSystemResizePending, Status: corev1.ConditionTrue},
		}}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "y"}},
	}
	items := []VolumeItem{
		{Kind: "pvc", Namespace: "a", PVC: "x", StorageClass: "sc-ok"},
		{Kind: "pvc", Namespace: "a", PVC: "y", StorageClass: "sc-no", UsageClass: "critical"},
		{Kind: "pvc", Namespace: "a", PVC: "z", StorageClass: "desconhecida"},
	}
	var sum VolumesSummary
	applyExpansion(items, pvcs, map[string]bool{"sc-ok": true, "sc-no": false}, &sum)
	markExpandBlocked(items, &sum)
	if items[0].CanExpand == nil || !*items[0].CanExpand || len(items[0].Issues) != 1 || sum.ResizePending != 1 {
		t.Fatalf("x: %+v %+v", items[0], sum)
	}
	if items[1].CanExpand == nil || *items[1].CanExpand || len(items[1].Issues) != 1 || sum.ExpandBlocked != 1 {
		t.Fatalf("y: %+v %+v", items[1], sum)
	}
	if items[2].CanExpand != nil {
		t.Fatalf("z deveria ficar sem CanExpand")
	}
}

func TestParseSnapshots(t *testing.T) {
	raw := []byte(`{"items":[
	 {"metadata":{"namespace":"a"},"spec":{"source":{"persistentVolumeClaimName":"x"}},"status":{"readyToUse":true,"creationTime":"2026-01-02T00:00:00Z"}},
	 {"metadata":{"namespace":"a"},"spec":{"source":{"persistentVolumeClaimName":"x"}},"status":{"readyToUse":true,"creationTime":"2026-01-05T00:00:00Z"}},
	 {"metadata":{"namespace":"a"},"spec":{"source":{"persistentVolumeClaimName":"x"}},"status":{"readyToUse":false}}]}`)
	m, ok := parseSnapshots(raw)
	if !ok || m["a/x"].Count != 2 || m["a/x"].Last.Day() != 5 {
		t.Fatalf("m=%+v ok=%v", m, ok)
	}
	if _, ok := parseSnapshots([]byte("não é json")); ok {
		t.Fatal("json inválido deveria falhar")
	}
	items := []VolumeItem{{Kind: "pvc", Namespace: "a", PVC: "x"}, {Kind: "pvc", Namespace: "a", PVC: "w"}}
	var sum VolumesSummary
	applySnapshots(items, m, &sum)
	if items[0].Snapshots != 2 || items[1].Snapshots != 0 || sum.WithSnapshot != 1 || !sum.SnapshotsKnown {
		t.Fatalf("%+v %+v", items, sum)
	}
}
