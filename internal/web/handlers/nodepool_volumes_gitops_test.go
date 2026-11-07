package handlers

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDetectManager(t *testing.T) {
	cases := []struct {
		labels, ann map[string]string
		want        string
	}{
		{map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"}, nil, "Flux Kustomization flux-system/apps"},
		{map[string]string{"helm.toolkit.fluxcd.io/name": "kafka"}, nil, "Flux HelmRelease kafka"},
		{map[string]string{"argocd.argoproj.io/instance": "shop"}, nil, "Argo CD shop"},
		{nil, map[string]string{"argocd.argoproj.io/tracking-id": "shop:apps/StatefulSet:ns/db"}, "Argo CD shop"},
		{nil, map[string]string{"meta.helm.sh/release-name": "pg"}, "Helm pg"},
		{map[string]string{"app.kubernetes.io/managed-by": "Helm"}, nil, "Helm"},
		{map[string]string{"app": "x"}, nil, ""},
	}
	for _, c := range cases {
		if got := detectManager(c.labels, c.ann); got != c.want {
			t.Errorf("%v %v: got %q, want %q", c.labels, c.ann, got, c.want)
		}
	}
}

func TestApplyManagement(t *testing.T) {
	pvcs := []corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "data-db-0"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "a", Name: "solto", Labels: map[string]string{"argocd.argoproj.io/instance": "x"}}},
	}
	specs := []workloadSpec{{
		Namespace: "a", Kind: "StatefulSet", Name: "db", ClaimTemplates: []string{"data"}, ClaimSizes: map[string]string{"data": "10Gi"},
		Labels: map[string]string{"kustomize.toolkit.fluxcd.io/name": "apps"},
	}}
	items := []VolumeItem{
		{Kind: "pvc", Namespace: "a", PVC: "data-db-0", Workloads: []string{"StatefulSet/db"}},
		{Kind: "pvc", Namespace: "a", PVC: "solto"},
		{Kind: "pvc", Namespace: "a", PVC: "data-db-x", Workloads: []string{"StatefulSet/db"}}, // sufixo não é ordinal
	}
	applyManagement(items, pvcs, specs)
	if items[0].ManagedBy != "Flux Kustomization apps" || items[0].TemplateSize != "10Gi" || items[0].TemplateOwner != "db" {
		t.Fatalf("sts: %+v", items[0])
	}
	if items[1].ManagedBy != "Argo CD x" || items[1].TemplateSize != "" {
		t.Fatalf("pvc solto: %+v", items[1])
	}
	if items[2].TemplateSize != "" {
		t.Fatalf("sufixo não ordinal não é do template: %+v", items[2])
	}
}

func TestApplyIdleSince(t *testing.T) {
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	items := []VolumeItem{{Kind: "pvc", Namespace: "a", PVC: "x", Idle: true}, {Kind: "pvc", Namespace: "a", PVC: "y", Idle: false}}
	first := map[string]time.Time{idleKey(items[0]): now.AddDate(0, 0, -12), idleKey(items[1]): now.AddDate(0, 0, -3)}
	applyIdleSince(items, first, now)
	if items[0].IdleDays == nil || *items[0].IdleDays != 12 || items[0].IdleSince == "" {
		t.Fatalf("x: %+v", items[0])
	}
	if items[1].IdleDays != nil {
		t.Fatalf("volume em uso não tem ociosidade: %+v", items[1])
	}
}
