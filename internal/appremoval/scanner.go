package appremoval

import (
	"fmt"
	"sort"
	"strings"
)

type scanner struct {
	release string
	byKey   map[string]*Resource
	items   []Item
	idx     map[string]int
}

func key(kind, name string) string { return kind + "/" + name }

// Scan monta o conjunto de recursos que pertencem à aplicação `name` no namespace. `name` pode
// ser o nome de um workload ou de um release Helm. Não altera nada.
func Scan(namespace, name string, resources []Resource) (*Result, error) {
	s := &scanner{
		byKey: make(map[string]*Resource, len(resources)),
		idx:   map[string]int{},
	}
	for i := range resources {
		s.byKey[key(resources[i].Kind, resources[i].Name)] = &resources[i]
	}

	roots := s.findRoots(resources, name)
	if len(roots) == 0 {
		return nil, ErrNotFound
	}

	res := &Result{Namespace: namespace, Name: name, Warnings: []string{}}
	if s.release != "" {
		res.Origin = OriginHelm
		res.Helm = &HelmInfo{Release: s.release, Revisions: countHelmRevisions(resources, s.release)}
		for i := range resources {
			r := &resources[i]
			if r.Annotations[annHelmRelease] == s.release && !isHelmStorage(r) {
				s.add(r, fmt.Sprintf("gerenciado pelo release Helm %s", s.release))
			}
		}
	} else {
		res.Origin = originOf(roots[0])
		for _, r := range roots {
			s.add(r, "workload informado")
		}
	}

	workloads := s.memberWorkloads()
	s.addReferences(workloads)
	s.addExposure()
	s.addAttached()
	s.addRBAC()
	s.addExternalSecrets()
	s.markShared(resources, workloads)

	res.Items = s.finish(res)
	res.Warnings = s.warnings(res)
	return res, nil
}

// findRoots acha os workloads com o nome informado; sem isso, trata `name` como release Helm.
func (s *scanner) findRoots(resources []Resource, name string) []*Resource {
	var roots []*Resource
	for i := range resources {
		r := &resources[i]
		if isWorkloadKind(r.Kind) && r.Name == name {
			roots = append(roots, r)
		}
	}
	for _, r := range roots {
		if rel := r.Annotations[annHelmRelease]; rel != "" {
			s.release = rel
			return roots
		}
	}
	if len(roots) > 0 {
		return roots
	}
	for i := range resources {
		r := &resources[i]
		if isWorkloadKind(r.Kind) && r.Annotations[annHelmRelease] == name {
			s.release = name
			roots = append(roots, r)
		}
	}
	return roots
}

// add registra o recurso como membro (uma vez só) e devolve o item.
func (s *scanner) add(r *Resource, reason string) *Item {
	k := key(r.Kind, r.Name)
	if i, ok := s.idx[k]; ok {
		return &s.items[i]
	}
	it := Item{
		Kind:   r.Kind,
		Name:   r.Name,
		UID:    r.UID,
		Origin: originOf(r),
		Reason: reason,
	}
	if s.release != "" && r.Annotations[annHelmRelease] == s.release {
		it.Origin = OriginHelm
		if r.Annotations[annHelmPolicy] == valueHelmPolicy {
			it.Reason += " (retido pelo Helm: resource-policy keep)"
		} else {
			it.RemovedByHelm = true
		}
	}
	s.idx[k] = len(s.items)
	s.items = append(s.items, it)
	return &s.items[len(s.items)-1]
}

func (s *scanner) has(kind, name string) bool {
	_, ok := s.idx[key(kind, name)]
	return ok
}

func (s *scanner) memberWorkloads() []workload {
	var out []workload
	for _, it := range s.items {
		if isWorkloadKind(it.Kind) {
			out = append(out, decodeWorkload(s.byKey[key(it.Kind, it.Name)]))
		}
	}
	return out
}

// addReferences inclui o que os pods montam/consomem: ConfigMap, Secret, PVC, SA e os PVCs de
// volumeClaimTemplates (que o StatefulSet cria e nenhum chart Helm remove).
func (s *scanner) addReferences(workloads []workload) {
	for _, w := range workloads {
		by := fmt.Sprintf("referenciado por %s/%s", w.res.Kind, w.res.Name)
		for _, n := range w.refs.configMaps {
			s.addIfExists(KindConfigMap, n, by)
		}
		for _, n := range w.refs.secrets {
			s.addIfExists(KindSecret, n, by)
		}
		for _, n := range w.refs.pvcs {
			s.addIfExists(KindPVC, n, by)
		}
		if w.refs.sa != "" {
			s.addIfExists(KindSA, w.refs.sa, by)
		}
		for _, vct := range w.vctNames {
			prefix := vct + "-" + w.res.Name + "-"
			for _, r := range s.sorted() {
				if r.Kind == KindPVC && strings.HasPrefix(r.Name, prefix) {
					s.add(r, fmt.Sprintf("volumeClaimTemplate %q do StatefulSet %s", vct, w.res.Name))
				}
			}
		}
	}
}

func (s *scanner) addIfExists(kind, name, reason string) {
	if r, ok := s.byKey[key(kind, name)]; ok && !isHelmStorage(r) {
		s.add(r, reason)
	}
}

// addExposure inclui Services (por selector) e o que os expõe: Ingress, HTTPRoute, VirtualService.
func (s *scanner) addExposure() {
	allPodLabels := s.memberPodLabels()
	for _, r := range s.sorted() {
		if r.Kind != KindService {
			continue
		}
		sel := serviceSelector(r)
		if matchesAny(sel, allPodLabels) {
			s.add(r, "selector bate com os pods da aplicação")
		}
	}
	for _, r := range s.sorted() {
		if r.Kind != KindIngress && r.Kind != KindHTTPRoute && r.Kind != KindVirtualService {
			continue
		}
		var mine, others []string
		for _, svc := range dedupe(backendServices(r)) {
			if s.has(KindService, svc) {
				mine = append(mine, svc)
			} else {
				others = append(others, svc)
			}
		}
		if len(mine) == 0 {
			continue
		}
		it := s.add(r, "encaminha para o Service "+strings.Join(mine, ", "))
		if len(others) > 0 {
			it.Shared = true
			it.UsedBy = append(it.UsedBy, others...)
		}
	}
}

// addAttached inclui HPA, PDB, NetworkPolicy, ServiceMonitor e PodMonitor ligados à aplicação.
func (s *scanner) addAttached() {
	allPodLabels := s.memberPodLabels()
	var svcLabels []map[string]string
	for _, it := range s.items {
		if it.Kind == KindService {
			svcLabels = append(svcLabels, s.byKey[key(it.Kind, it.Name)].Labels)
		}
	}
	for _, r := range s.sorted() {
		switch r.Kind {
		case KindHPA:
			kind, name := hpaTarget(r)
			if s.has(kind, name) {
				s.add(r, fmt.Sprintf("escala %s/%s", kind, name))
			}
		case KindPDB:
			if matchesAny(matchLabelsOf(r, "selector"), allPodLabels) {
				s.add(r, "selector bate com os pods da aplicação")
			}
		case KindNetworkPolicy:
			if matchesAny(matchLabelsOf(r, "podSelector"), allPodLabels) {
				s.add(r, "podSelector bate com os pods da aplicação")
			}
		case KindPodMonitor:
			if matchesAny(matchLabelsOf(r, "selector"), allPodLabels) {
				s.add(r, "selector bate com os pods da aplicação")
			}
		case KindServiceMonitor:
			if matchesAny(matchLabelsOf(r, "selector"), svcLabels) {
				s.add(r, "selector bate com um Service da aplicação")
			}
		}
	}
}

// addRBAC inclui RoleBindings cujos sujeitos são ServiceAccounts da aplicação e os Roles que eles usam.
func (s *scanner) addRBAC() {
	for _, r := range s.sorted() {
		if r.Kind != KindRoleBinding {
			continue
		}
		var others []string
		mine := false
		for _, sub := range r.Subjects {
			if sub.Kind == KindSA && s.has(KindSA, sub.Name) {
				mine = true
			} else {
				others = append(others, sub.Kind+"/"+sub.Name)
			}
		}
		if !mine {
			continue
		}
		it := s.add(r, "concede permissões à ServiceAccount da aplicação")
		if len(others) > 0 {
			it.Shared = true
			it.UsedBy = append(it.UsedBy, others...)
		}
		if r.RoleRefKind == KindRole {
			s.addIfExists(KindRole, r.RoleRefName, fmt.Sprintf("usado pelo RoleBinding %s", r.Name))
		}
	}
}

// addExternalSecrets inclui o ExternalSecret dono de um Secret da aplicação: sem removê-lo, o
// Secret volta sozinho.
func (s *scanner) addExternalSecrets() {
	for _, it := range append([]Item{}, s.items...) {
		if it.Kind != KindSecret {
			continue
		}
		for _, o := range s.byKey[key(it.Kind, it.Name)].Owners {
			if o.Kind != KindExternalSecret || s.has(KindExternalSecret, o.Name) {
				continue
			}
			s.idx[key(KindExternalSecret, o.Name)] = len(s.items)
			s.items = append(s.items, Item{
				Kind:   KindExternalSecret,
				Name:   o.Name,
				UID:    o.UID,
				Origin: OriginManual,
				Reason: fmt.Sprintf("gera o Secret %s (se mantido, o Secret é recriado)", it.Name),
			})
		}
	}
}

// markShared marca ConfigMap/Secret/PVC/SA também usados por workloads de fora da aplicação.
func (s *scanner) markShared(resources []Resource, members []workload) {
	own := map[string]bool{}
	for _, w := range members {
		own[key(w.res.Kind, w.res.Name)] = true
	}
	users := map[string][]string{}
	for i := range resources {
		r := &resources[i]
		if !isWorkloadKind(r.Kind) || own[key(r.Kind, r.Name)] {
			continue
		}
		w := decodeWorkload(r)
		by := r.Kind + "/" + r.Name
		for _, n := range w.refs.configMaps {
			users[key(KindConfigMap, n)] = append(users[key(KindConfigMap, n)], by)
		}
		for _, n := range w.refs.secrets {
			users[key(KindSecret, n)] = append(users[key(KindSecret, n)], by)
		}
		for _, n := range w.refs.pvcs {
			users[key(KindPVC, n)] = append(users[key(KindPVC, n)], by)
		}
		if w.refs.sa != "" {
			users[key(KindSA, w.refs.sa)] = append(users[key(KindSA, w.refs.sa)], by)
		}
	}
	for i := range s.items {
		if u := users[key(s.items[i].Kind, s.items[i].Name)]; len(u) > 0 {
			s.items[i].Shared = true
			s.items[i].UsedBy = dedupe(append(s.items[i].UsedBy, u...))
		}
	}
}

// finish aplica a seleção sugerida, adiciona o item do release Helm e ordena por ordem de remoção.
func (s *scanner) finish(res *Result) []Item {
	items := s.items
	for i := range items {
		it := &items[i]
		it.DataRisk = it.Kind == KindSecret || it.Kind == KindPVC
		switch {
		case it.Shared, it.RemovedByHelm:
			it.DefaultSelected = false
		case it.Kind == KindSecret, it.Kind == KindPVC, it.Kind == KindExternalSecret:
			it.DefaultSelected = false
		default:
			it.DefaultSelected = true
		}
	}
	if res.Helm != nil {
		removed := 0
		for _, it := range items {
			if it.RemovedByHelm {
				removed++
			}
		}
		items = append(items, Item{
			Kind:   KindHelmRelease,
			Name:   res.Helm.Release,
			Origin: OriginHelm,
			Reason: fmt.Sprintf("helm uninstall remove %d recursos do manifesto (%d revisões no histórico)",
				removed, res.Helm.Revisions),
			DefaultSelected: true,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := removalRank(items[i].Kind), removalRank(items[j].Kind)
		if ri != rj {
			return ri < rj
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func (s *scanner) warnings(res *Result) []string {
	w := []string{}
	if res.Helm != nil {
		var data []string
		for _, it := range res.Items {
			if it.RemovedByHelm && it.DataRisk {
				data = append(data, it.Kind+"/"+it.Name)
			}
		}
		var sharedHelm []string
		for _, it := range res.Items {
			if it.RemovedByHelm && it.Shared {
				sharedHelm = append(sharedHelm, it.Kind+"/"+it.Name)
			}
		}
		if len(sharedHelm) > 0 {
			w = append(w, "ATENÇÃO: o helm uninstall removerá recursos que outra aplicação também usa: "+
				strings.Join(sharedHelm, ", ")+".")
		}
		if len(data) > 0 {
			w = append(w, "O helm uninstall também remove do cluster: "+strings.Join(data, ", ")+
				". Não dá para manter só esses itens sem reter o release.")
		}
	}
	if res.Origin == OriginKustomizeProvavel {
		w = append(w, "Origem inferida por labels e annotation do kubectl apply: não há marca de Kustomize no cluster. "+
			"Confirme se algum pipeline reaplica esses manifestos.")
	}
	shared := 0
	for _, it := range res.Items {
		if it.Shared {
			shared++
		}
	}
	if shared > 0 {
		w = append(w, fmt.Sprintf("%d item(ns) também usado(s) por outra aplicação: vêm desmarcados.", shared))
	}
	return w
}

// ─── helpers ────────────────────────────────────────────────────────────────

func (s *scanner) memberPodLabels() []map[string]string {
	var out []map[string]string
	for _, it := range s.items {
		if isWorkloadKind(it.Kind) {
			if l := decodeWorkload(s.byKey[key(it.Kind, it.Name)]).podLabels; len(l) > 0 {
				out = append(out, l)
			}
		}
	}
	return out
}

// sorted devolve todos os recursos em ordem estável (o mapa não tem ordem).
func (s *scanner) sorted() []*Resource {
	out := make([]*Resource, 0, len(s.byKey))
	for _, r := range s.byKey {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// matchesAny: seletor não vazio cujos pares estão todos em algum dos conjuntos de labels.
func matchesAny(sel map[string]string, sets []map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for _, labels := range sets {
		ok := true
		for k, v := range sel {
			if labels[k] != v {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func isHelmStorage(r *Resource) bool {
	return r.Kind == KindSecret && r.Type == helmSecretType
}

func countHelmRevisions(resources []Resource, release string) int {
	n := 0
	for i := range resources {
		r := &resources[i]
		if isHelmStorage(r) && r.Labels["name"] == release {
			n++
		}
	}
	return n
}

// originOf é a heurística de gerenciador para recursos sem anotação do Helm.
func originOf(r *Resource) Origin {
	if r.Annotations[annHelmRelease] != "" {
		return OriginHelm
	}
	if r.Labels[labelManagedBy] == valueKustomize {
		return OriginKustomize
	}
	_, applied := r.Annotations[annLastApplied]
	hasAppLabel := r.Labels[labelAppName] != "" || r.Labels[labelAppInst] != "" || r.Labels[labelLegacyApp] != ""
	if applied && hasAppLabel {
		return OriginKustomizeProvavel
	}
	return OriginManual
}

// removalRank: exposição primeiro, depois o que se anexa ao workload, workload, Service e por fim config/dados/RBAC.
func removalRank(kind string) int {
	switch kind {
	case KindHelmRelease:
		return 0
	case KindIngress, KindHTTPRoute, KindVirtualService:
		return 1
	case KindHPA, KindPDB, KindNetworkPolicy, KindServiceMonitor, KindPodMonitor:
		return 2
	case KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob:
		return 3
	case KindService:
		return 4
	case KindRoleBinding:
		return 6
	case KindRole:
		return 7
	case KindSA:
		return 8
	default:
		return 5
	}
}
