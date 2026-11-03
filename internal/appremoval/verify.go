package appremoval

import (
	"fmt"
	"sort"
)

// Estados de um item na verificação pós-remoção.
const (
	StateRemoved     = "removed"
	StateRemaining   = "remaining"
	StateTerminating = "terminating"
	StateRecreated   = "recreated"
	StateNew         = "new"
)

// VerifyResult compara o scan original com o estado atual do namespace.
type VerifyResult struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// AppGone: nenhum item original restou e nada novo apareceu.
	AppGone bool `json:"app_gone"`
	// Items são todos os itens do scan original, com State.
	Items []Item `json:"items"`
	// Leftovers é o que ainda existe (sobrou, Terminating, recriado ou novo), pronto para uma nova
	// rodada de seleção; nil quando AppGone.
	Leftovers *Result `json:"leftovers,omitempty"`
}

// Settling indica itens ainda em Terminating: vale esperar um pouco e verificar de novo.
func (v *VerifyResult) Settling() bool {
	for _, it := range v.Items {
		if it.State == StateTerminating {
			return true
		}
	}
	return false
}

// Verify procura cada item do scan original nos recursos atuais. Parte dos itens originais (e não
// de um novo Scan) porque, sem os workloads, nada mais leva aos PVCs/Secrets/ConfigMaps que sobraram.
// Também aponta o que apareceu de novo (recriado por operador/pipeline).
func Verify(original *Result, resources []Resource) *VerifyResult {
	byKey := make(map[string]*Resource, len(resources))
	for i := range resources {
		byKey[key(resources[i].Kind, resources[i].Name)] = &resources[i]
	}
	releaseRevisions := 0
	if original.Helm != nil {
		releaseRevisions = countHelmRevisions(resources, original.Helm.Release)
	}
	helmGone := original.Helm != nil && releaseRevisions == 0

	v := &VerifyResult{Namespace: original.Namespace, Name: original.Name, Items: []Item{}}
	known := make(map[string]bool, len(original.Items))
	var left []Item

	for _, it := range original.Items {
		known[key(it.Kind, it.Name)] = true
		cur := it
		cur.State = StateRemoved
		if it.Kind == KindHelmRelease {
			if releaseRevisions > 0 {
				cur.State = StateRemaining
			}
		} else if r, ok := byKey[key(it.Kind, it.Name)]; ok {
			switch {
			case it.UID != "" && r.UID != "" && it.UID != r.UID:
				cur.State = StateRecreated
			case r.DeletionTimestamp != "":
				cur.State = StateTerminating
				cur.Finalizers = r.Finalizers
			default:
				cur.State = StateRemaining
			}
			cur.UID = r.UID
		}
		v.Items = append(v.Items, cur)
		if cur.State == StateRemoved {
			continue
		}
		if helmGone && cur.RemovedByHelm {
			cur.RemovedByHelm = false
			cur.Reason += " (release Helm já removido)"
		}
		cur.DefaultSelected = defaultSelected(cur)
		left = append(left, cur)
	}

	left = append(left, newItems(original, resources, known)...)
	if len(left) == 0 {
		v.AppGone = true
		return v
	}

	sort.SliceStable(left, func(i, j int) bool {
		ri, rj := removalRank(left[i].Kind), removalRank(left[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return left[i].Name < left[j].Name
	})
	res := &Result{
		Namespace: original.Namespace,
		Name:      original.Name,
		Origin:    original.Origin,
		Items:     left,
		Warnings:  leftoverWarnings(left),
	}
	if releaseRevisions > 0 {
		res.Helm = &HelmInfo{Release: original.Helm.Release, Revisions: releaseRevisions}
	}
	v.Leftovers = res
	return v
}

// newItems acha o que não existia no scan original: se os workloads voltaram, o Scan os enxerga;
// senão, em apps Helm, qualquer recurso ainda anotado com o release.
func newItems(original *Result, resources []Resource, known map[string]bool) []Item {
	var out []Item
	if sc, err := Scan(original.Namespace, original.Name, resources); err == nil {
		for _, it := range sc.Items {
			if it.Kind != KindHelmRelease && !known[key(it.Kind, it.Name)] {
				it.State = StateNew
				out = append(out, it)
			}
		}
		return out
	}
	if original.Helm == nil {
		return nil
	}
	for i := range resources {
		r := &resources[i]
		if r.Annotations[annHelmRelease] != original.Helm.Release || isHelmStorage(r) ||
			known[key(r.Kind, r.Name)] || resourceFor(r.Kind) == "" {
			continue
		}
		it := Item{
			Kind: r.Kind, Name: r.Name, UID: r.UID, Origin: OriginHelm, State: StateNew,
			Reason:   fmt.Sprintf("ainda anotado com o release Helm %s", original.Helm.Release),
			DataRisk: r.Kind == KindSecret || r.Kind == KindPVC,
		}
		it.DefaultSelected = defaultSelected(it)
		out = append(out, it)
	}
	return out
}

func leftoverWarnings(items []Item) []string {
	w := []string{}
	var terminating, recreated, fresh int
	for _, it := range items {
		switch it.State {
		case StateTerminating:
			terminating++
		case StateRecreated:
			recreated++
		case StateNew:
			fresh++
		}
	}
	if terminating > 0 {
		w = append(w, fmt.Sprintf("%d item(ns) em Terminating: um finalizer pode estar impedindo a remoção.", terminating))
	}
	if recreated > 0 {
		w = append(w, fmt.Sprintf("%d item(ns) foi(ram) recriado(s) depois da remoção: algum operador ou pipeline pode estar reaplicando.", recreated))
	}
	if fresh > 0 {
		w = append(w, fmt.Sprintf("%d item(ns) novo(s) apareceu(ram) depois do scan original.", fresh))
	}
	return w
}
