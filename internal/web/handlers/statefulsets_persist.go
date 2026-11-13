package handlers

import (
	"context"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Um StatefulSet criado por um operator (ex.: prometheus-operator) é reconciliado pelo dono: o operator reescreve
// o spec a partir do recurso dele (o CRD Prometheus, que por sua vez vem dos values do Helm) em segundos. O apply do
// YAML "dá certo" (HTTP 200), mas a alteração some. Estas funções identificam o dono e conferem, depois do apply,
// se o spec foi alterado por outro gerenciador.

// revertCheckDelay é quanto o Apply espera antes de reler o StatefulSet de um recurso com dono (variável para testes).
var revertCheckDelay = 5 * time.Second

// ourFieldManagers são os gerenciadores do próprio apply (kubectl apply --server-side usa "kubectl").
var ourFieldManagers = map[string]bool{"kubectl": true, "kubectl-client-side-apply": true}

type statefulSetOwner struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// controllerOwner devolve o dono controlador do StatefulSet (ownerReferences com controller=true), ou nil.
func controllerOwner(sts *appsv1.StatefulSet) *statefulSetOwner {
	if sts == nil {
		return nil
	}
	if ref := metav1.GetControllerOf(sts); ref != nil {
		return &statefulSetOwner{Kind: ref.Kind, Name: ref.Name}
	}
	return nil
}

// detectRevert diz se o spec foi alterado por outro gerenciador depois do nosso apply. genBefore é a geração antes
// do apply, genApplied a lida logo depois e after o objeto relido após a espera. Só conta como "revertido" se a
// geração andou além do que o nosso apply explica (uma alteração do spec = uma geração a mais) E existe outro
// gerenciador escrevendo desde o início do apply; devolve quem foi.
func detectRevert(genBefore, genApplied int64, after *appsv1.StatefulSet, since time.Time) (bool, string) {
	if after == nil {
		return false, ""
	}
	modified := after.Generation > genApplied || genApplied > genBefore+1
	if !modified {
		return false, ""
	}
	writer := lastForeignWriter(after, since)
	return writer != "", writer
}

// lastForeignWriter devolve o gerenciador (que não é o nosso) com a escrita mais recente a partir de since.
func lastForeignWriter(sts *appsv1.StatefulSet, since time.Time) string {
	var best time.Time
	var name string
	for _, mf := range sts.ManagedFields {
		if mf.Time == nil || strings.TrimSpace(mf.Manager) == "" || ourFieldManagers[mf.Manager] {
			continue
		}
		if mf.Time.Time.Before(since) {
			continue
		}
		if mf.Time.Time.After(best) {
			best, name = mf.Time.Time, mf.Manager
		}
	}
	return name
}

// sleepCtx espera d ou até o ctx acabar; false se o ctx acabou antes.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
