package finops

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podOwnedBy(kind, name string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pod-abc12"}}
	if kind != "" {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: name}}
	}
	return p
}

func TestResolveWorkloadKind(t *testing.T) {
	rsOwner := map[string]string{"ns/api-7d9f8b6c5": "api"}
	cases := []struct {
		pod  *corev1.Pod
		want string
	}{
		{podOwnedBy("ReplicaSet", "api-7d9f8b6c5"), "Deployment"},
		{podOwnedBy("ReplicaSet", "solto-7d9f8b6c5"), "ReplicaSet"},
		{podOwnedBy("StatefulSet", "kafka"), "StatefulSet"},
		{podOwnedBy("DaemonSet", "fluentbit"), "DaemonSet"},
		{podOwnedBy("Job", "migracao"), "Job"},
		{podOwnedBy("", ""), "Pod"},
	}
	for _, c := range cases {
		if got := ResolveWorkloadKind(c.pod, rsOwner); got != c.want {
			t.Errorf("%v: got %q, want %q", c.pod.OwnerReferences, got, c.want)
		}
	}
}
