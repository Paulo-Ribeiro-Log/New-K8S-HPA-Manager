package handlers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestExtractSquads(t *testing.T) {
	labels := map[string]string{
		"kubernetes.io/metadata.name":                           "faturamento-prd",
		squadKeyPrefix + "96dfaea1-97ff-44ab-8c05-de28d77c5d5d": "",
		squadKeyPrefix + "10075":                                "",
	}
	annotations := map[string]string{
		squadKeyPrefix + "96dfaea1-97ff-44ab-8c05-de28d77c5d5d": "Documentos Logísticos\n  E Expedição",
		squadKeyPrefix + "10075":                                "Documentos Logisticos e Expedição",
		"outra/annotation":                                      "x",
	}
	got := extractSquads("faturamento-prd", labels, annotations)
	if len(got) != 2 {
		t.Fatalf("esperava 2 squads, veio %+v", got)
	}
	if got[0].ID != "10075" || !got[0].InLabel || !got[0].InAnnotation {
		t.Errorf("squad 10075 = %+v", got[0])
	}
	if got[1].Name != "Documentos Logísticos E Expedição" {
		t.Errorf("nome com quebra de linha deveria virar espaço simples: %q", got[1].Name)
	}
}

func TestBuildRBACOverview(t *testing.T) {
	group := "96dfaea1-97ff-44ab-8c05-de28d77c5d5d"
	cs := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "faturamento-prd",
			Annotations: map[string]string{squadKeyPrefix + group: "Squad X"}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "squad-edit", Namespace: "faturamento-prd"},
			RoleRef:  rbacv1.RoleRef{Kind: "ClusterRole", Name: "edit"},
			Subjects: []rbacv1.Subject{{Kind: "Group", Name: group}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "faturamento-prd"},
			RoleRef: rbacv1.RoleRef{Kind: "Role", Name: "leitor"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "admins"},
			RoleRef:  rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
			Subjects: []rbacv1.Subject{{Kind: "Group", Name: "admins-guid"}}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "edit"},
			Rules: []rbacv1.PolicyRule{{Verbs: []string{"get", "update"}, APIGroups: []string{"apps"}, Resources: []string{"deployments"}}}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "nao-referenciada"}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "leitor", Namespace: "faturamento-prd"},
			Rules: []rbacv1.PolicyRule{{Verbs: []string{"get"}, Resources: []string{"pods"}}}},
	)

	ov, err := buildRBACOverview(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Squads) != 1 || ov.Squads[0].ID != group || ov.Squads[0].Name != "Squad X" {
		t.Errorf("squads = %+v", ov.Squads)
	}
	if len(ov.Bindings) != 3 {
		t.Errorf("bindings = %+v", ov.Bindings)
	}
	if _, ok := ov.Roles["ClusterRole/edit"]; !ok {
		t.Error("ClusterRole edit referenciada deveria vir")
	}
	if _, ok := ov.Roles["Role/faturamento-prd/leitor"]; !ok {
		t.Error("Role leitor referenciada deveria vir")
	}
	if _, ok := ov.Roles["ClusterRole/nao-referenciada"]; ok {
		t.Error("ClusterRole não referenciada não deveria vir")
	}
	if _, ok := ov.Roles["ClusterRole/cluster-admin"]; ok {
		t.Error("cluster-admin não existe no fake — deveria ficar ausente (role inexistente)")
	}
}
