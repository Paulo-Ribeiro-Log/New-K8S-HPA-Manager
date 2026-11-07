package kubernetes

import (
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
)

func TestApplyResourceRuleVolumes(t *testing.T) {
	rule := func(res string, verbs ...string) authorizationv1.ResourceRule {
		return authorizationv1.ResourceRule{APIGroups: []string{""}, Resources: []string{res}, Verbs: verbs}
	}
	var p NamespacePermissions
	applyResourceRule(rule("persistentvolumeclaims", "get", "patch"), &p)
	if !p.CanPatchPVC || p.CanDeletePVC || p.CanDeletePV {
		t.Fatalf("patch PVC: %+v", p)
	}
	applyResourceRule(rule("persistentvolumeclaims", "delete"), &p)
	applyResourceRule(rule("persistentvolumes", "delete"), &p)
	if !p.CanDeletePVC || !p.CanDeletePV {
		t.Fatalf("delete: %+v", p)
	}
	var q NamespacePermissions
	applyResourceRule(rule("persistentvolumes", "get", "list"), &q)
	if q.CanDeletePV {
		t.Fatalf("só leitura não pode apagar PV: %+v", q)
	}
	var w NamespacePermissions
	applyResourceRule(authorizationv1.ResourceRule{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}}, &w)
	if !w.CanPatchPVC || !w.CanDeletePVC || !w.CanDeletePV {
		t.Fatalf("wildcard: %+v", w)
	}
}
