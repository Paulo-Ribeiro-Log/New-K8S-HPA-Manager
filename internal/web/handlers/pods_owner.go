package handlers

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// podOwnedBy diz se o pod pertence ao workload `owner` (ex.: "Deployment/api"), pelo dono real
// (OwnerReferences, via resolveOwnerDisplayName) e não pelo nome do pod.
//
// Por que não por nome: o nome do pod é truncado quando o do Deployment é longo, e o prefixo
// "<deploy>-" também casa com Deployments da mesma família ("api" e "api-worker"). O mesmo vale
// para o parâmetro `search` de GET /pods, que casa por nome do pod, namespace, node e container.
func podOwnedBy(pod *corev1.Pod, owner string) bool {
	got := resolveOwnerDisplayName(pod)
	if got == owner {
		return true
	}
	// Deployment cujo ReplicaSet tem um sufixo que não parece hash: o dono sai como
	// "ReplicaSet/<deploy>-<hash>". O hash nunca tem hífen, então "api-worker-5d8f" não é "api".
	if name, ok := strings.CutPrefix(owner, "Deployment/"); ok {
		if rest, ok := strings.CutPrefix(got, "ReplicaSet/"+name+"-"); ok {
			return rest != "" && !strings.Contains(rest, "-")
		}
	}
	return false
}
