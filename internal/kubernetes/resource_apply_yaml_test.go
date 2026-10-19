package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourceApplyYAMLTemApiVersionKindEEscopoMinimo(t *testing.T) {
	replicas := int32(2)
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi")},
	}
	out, err := resourceApplyYAML("StatefulSet", "monitoring", "prometheus-x", "prometheus", &replicas, res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"apiVersion: apps/v1", "kind: StatefulSet", "name: prometheus-x", "namespace: monitoring",
		"replicas: 2", "name: prometheus", "memory: 4Gi", "memory: 8Gi"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
	for _, no := range []string{"managedFields", "status", "resourceVersion", "creationTimestamp: null"} {
		if strings.Contains(out, no) {
			t.Errorf("não devia conter %q em:\n%s", no, out)
		}
	}
	// o payload tem que passar pela mesma validação que o kubectl faz antes do apply
	if _, ns, name, err := prepareStatefulSetApplyPayload(out, "monitoring", "prometheus-x"); err != nil || ns != "monitoring" || name != "prometheus-x" {
		t.Errorf("prepareStatefulSetApplyPayload: ns=%q name=%q err=%v", ns, name, err)
	}
}

func TestResourceApplyYAMLSemReplicas(t *testing.T) {
	out, err := resourceApplyYAML("DaemonSet", "kube-system", "ds", "c", nil, corev1.ResourceRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "replicas") {
		t.Errorf("DaemonSet não tem replicas:\n%s", out)
	}
}
