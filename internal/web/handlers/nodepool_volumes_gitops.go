package handlers

import (
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Quem gerencia o volume: um controlador de GitOps (Flux, Argo CD) ou o Helm costuma reverter ou
// recriar mudanças manuais; o StatefulSet guarda o tamanho num volumeClaimTemplate imutável. Nada
// disso impede a ação — a tela só avisa antes, para o manifesto ser atualizado junto.

// detectManager devolve "Flux Kustomization ns/nome", "Flux HelmRelease ns/nome", "Argo CD nome",
// "Helm nome" ou "" pelas labels/annotations padrão de cada ferramenta.
func detectManager(labels, ann map[string]string) string {
	if n := labels["kustomize.toolkit.fluxcd.io/name"]; n != "" {
		return "Flux Kustomization " + joinNS(labels["kustomize.toolkit.fluxcd.io/namespace"], n)
	}
	if n := labels["helm.toolkit.fluxcd.io/name"]; n != "" {
		return "Flux HelmRelease " + joinNS(labels["helm.toolkit.fluxcd.io/namespace"], n)
	}
	if n := labels["argocd.argoproj.io/instance"]; n != "" {
		return "Argo CD " + n
	}
	if t := ann["argocd.argoproj.io/tracking-id"]; t != "" {
		return "Argo CD " + strings.SplitN(t, ":", 2)[0]
	}
	if n := ann["meta.helm.sh/release-name"]; n != "" {
		return "Helm " + n
	}
	if labels["app.kubernetes.io/managed-by"] == "Helm" {
		return "Helm"
	}
	return ""
}

func joinNS(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// templateFor acha o volumeClaimTemplate de um StatefulSet que originou o PVC "<template>-<sts>-<n>".
func templateFor(pvc string, sts workloadSpec) (size string, ok bool) {
	for _, t := range sts.ClaimTemplates {
		prefix := t + "-" + sts.Name + "-"
		if strings.HasPrefix(pvc, prefix) && isOrdinalSuffix(strings.TrimPrefix(pvc, prefix)) {
			return sts.ClaimSizes[t], true
		}
	}
	return "", false
}

// applyManagement preenche ManagedBy (do PVC; se não houver, do workload dono) e, para PVC de
// StatefulSet, o tamanho do volumeClaimTemplate.
func applyManagement(items []VolumeItem, pvcs []corev1.PersistentVolumeClaim, specs []workloadSpec) {
	pvcByKey := map[string]corev1.PersistentVolumeClaim{}
	for _, p := range pvcs {
		pvcByKey[p.Namespace+"/"+p.Name] = p
	}
	specByKey := map[string]workloadSpec{}
	for _, w := range specs {
		specByKey[w.Namespace+"/"+w.Kind+"/"+w.Name] = w
	}
	for i := range items {
		it := &items[i]
		if it.Kind != "pvc" {
			continue
		}
		if p, ok := pvcByKey[it.Namespace+"/"+it.PVC]; ok {
			it.ManagedBy = detectManager(p.Labels, p.Annotations)
		}
		for _, w := range it.Workloads {
			sp, ok := specByKey[it.Namespace+"/"+w]
			if !ok {
				continue
			}
			if it.ManagedBy == "" {
				it.ManagedBy = detectManager(sp.Labels, sp.Annotations)
			}
			if sp.Kind == "StatefulSet" && it.TemplateOwner == "" {
				if size, ok := templateFor(it.PVC, sp); ok {
					it.TemplateSize, it.TemplateOwner = size, sp.Name
				}
			}
		}
	}
}

// ---- ociosidade observada ----

// VolumeIdleTracker é o que o handler precisa do registro de ociosidade (storage.VolumeIdleStore).
type VolumeIdleTracker interface {
	Sync(cluster string, idleKeys []string, now time.Time) (map[string]time.Time, error)
}

func (h *NodePoolHandler) SetVolumeIdleTracker(t VolumeIdleTracker) { h.volumeIdle = t }

func idleKey(it VolumeItem) string {
	return it.Kind + "|" + it.Namespace + "|" + it.PVC + "|" + it.PV
}

// applyIdleSince grava a ociosidade atual e devolve em cada item desde quando foi vista.
func applyIdleSince(items []VolumeItem, first map[string]time.Time, now time.Time) {
	for i := range items {
		it := &items[i]
		if !it.Idle {
			continue
		}
		t, ok := first[idleKey(*it)]
		if !ok {
			continue
		}
		d := int(now.Sub(t).Hours() / 24)
		if d < 0 {
			d = 0
		}
		it.IdleSince = t.UTC().Format(time.RFC3339)
		it.IdleDays = &d
	}
}

func (h *NodePoolHandler) trackIdle(items []VolumeItem, cluster string, sum *VolumesSummary) {
	if h.volumeIdle == nil {
		return
	}
	var keys []string
	for _, it := range items {
		if it.Idle {
			keys = append(keys, idleKey(it))
		}
	}
	now := time.Now()
	first, err := h.volumeIdle.Sync(cluster, keys, now)
	if err != nil {
		return // o registro é enriquecimento: falhar não derruba a listagem
	}
	sum.IdleTracking = true
	applyIdleSince(items, first, now)
}
