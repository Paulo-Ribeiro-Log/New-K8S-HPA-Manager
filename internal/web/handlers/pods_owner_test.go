package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podWithOwner(name, kind, ownerName string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if kind != "" {
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: ownerName}}
	}
	return p
}

func TestPodOwnedBy(t *testing.T) {
	long := "calculofrete-api-integracao-externa-processamento-xyz"
	cases := []struct {
		name  string
		pod   *corev1.Pod
		owner string
		want  bool
	}{
		{"deployment dono", podWithOwner("api-5d8f7c9b6f-abcde", "ReplicaSet", "api-5d8f7c9b6f"), "Deployment/api", true},
		{"família de nome não é o mesmo deployment", podWithOwner("api-worker-7fd5cc8b9-x", "ReplicaSet", "api-worker-7fd5cc8b9"), "Deployment/api", false},
		{"nome do pod contém o texto, mas é de outro workload", podWithOwner("my-api-gateway-6b7d9c8f5-x", "ReplicaSet", "my-api-gateway-6b7d9c8f5"), "Deployment/api", false},
		{"statefulset com o mesmo nome", podWithOwner("api-0", "StatefulSet", "api"), "Deployment/api", false},
		{"job", podWithOwner("api-cron-123-x", "Job", "api-cron-123"), "Deployment/api", false},
		{"pod solto", podWithOwner("api-solto", "", ""), "Deployment/api", false},
		{"replicaset com sufixo curto (sem cara de hash)", podWithOwner("api-79d4b-x", "ReplicaSet", "api-79d4b"), "Deployment/api", true},
		{"replicaset curto de outro deployment", podWithOwner("api-worker-79d4b-x", "ReplicaSet", "api-worker-79d4b"), "Deployment/api", false},
		{"nome longo: o nome do pod é truncado, o dono não", podWithOwner(long[:50]+"-5d8f7c9b6f-abcde", "ReplicaSet", long+"-5d8f7c9b6f"), "Deployment/" + long, true},
		{"daemonset", podWithOwner("agent-x1", "DaemonSet", "agent"), "DaemonSet/agent", true},
	}
	for _, c := range cases {
		if got := podOwnedBy(c.pod, c.owner); got != c.want {
			t.Errorf("%s: podOwnedBy = %v, quer %v", c.name, got, c.want)
		}
	}
}
