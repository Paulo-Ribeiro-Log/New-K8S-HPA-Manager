package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateExpand(t *testing.T) {
	pvc := corev1.PersistentVolumeClaim{Status: corev1.PersistentVolumeClaimStatus{
		Phase: corev1.ClaimBound, Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
	}}
	if q, err := validateExpand(pvc, true, 20); err != nil || q.String() != "20Gi" {
		t.Fatalf("q=%v err=%v", q, err)
	}
	for name, c := range map[string]struct {
		allow bool
		gib   float64
	}{"sem expansão": {false, 20}, "igual": {true, 10}, "reduz": {true, 5}, "fracionado": {true, 20.5}, "zero": {true, 0}} {
		if _, err := validateExpand(pvc, c.allow, c.gib); err == nil {
			t.Errorf("%s: deveria falhar", name)
		}
	}
	pvc.Status.Phase = corev1.ClaimPending
	if _, err := validateExpand(pvc, true, 20); err == nil {
		t.Error("PVC Pending deveria falhar")
	}
}

func TestPodsUsingPVC(t *testing.T) {
	mk := func(ns, name, claim string, ph corev1.PodPhase) corev1.Pod {
		return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Status: corev1.PodStatus{Phase: ph},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}}}}
	}
	pods := []corev1.Pod{mk("a", "p1", "x", corev1.PodRunning), mk("a", "p2", "x", corev1.PodSucceeded), mk("b", "p3", "x", corev1.PodRunning), mk("a", "p4", "y", corev1.PodPending)}
	if got := podsUsingPVC(pods, "a", "x"); len(got) != 1 || got[0] != "p1" {
		t.Fatalf("got %v", got)
	}
}

func TestValidateDeletePV(t *testing.T) {
	pv := corev1.PersistentVolume{}
	for ph, ok := range map[corev1.PersistentVolumePhase]bool{corev1.VolumeReleased: true, corev1.VolumeAvailable: true, corev1.VolumeBound: false} {
		pv.Status.Phase = ph
		if err := validateDeletePV(pv); (err == nil) != ok {
			t.Errorf("%s: err=%v", ph, err)
		}
	}
}
