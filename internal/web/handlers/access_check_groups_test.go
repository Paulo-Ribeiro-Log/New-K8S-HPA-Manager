package handlers

import (
	"context"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	gidAdmin = "09e0fe45-a425-4090-9d19-935e3d01e79d"
	gidDev   = "11111111-2222-3333-4444-555555555555"
)

func groupSubject(name string) rbacv1.Subject {
	return rbacv1.Subject{Kind: rbacv1.GroupKind, Name: name}
}

func findGroup(t *testing.T, gs []ClusterGroup, id string) ClusterGroup {
	t.Helper()
	for _, g := range gs {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("grupo %q não encontrado em %+v", id, gs)
	return ClusterGroup{}
}

func TestMergeClusterGroupsJuntaAdminEBindings(t *testing.T) {
	bindings := []ClusterGroupBinding{
		{Kind: "ClusterRoleBinding", Name: "admins", RoleKind: "ClusterRole", RoleName: "cluster-admin"},
		{Kind: "RoleBinding", Name: "dev", Namespace: "app", RoleKind: "ClusterRole", RoleName: "edit"},
	}
	subjects := [][]rbacv1.Subject{
		{groupSubject("09E0FE45-A425-4090-9D19-935E3D01E79D"), {Kind: rbacv1.UserKind, Name: "fulano@x.com"}},
		{groupSubject(gidDev), groupSubject("az_c_squad_x"), groupSubject("  ")},
	}
	got := mergeClusterGroups([]string{gidAdmin}, bindings, subjects)

	if len(got) != 3 {
		t.Fatalf("esperava 3 grupos (admin, dev, nome), veio %d: %+v", len(got), got)
	}
	admin := findGroup(t, got, gidAdmin)
	if len(admin.Sources) != 2 || !admin.IsObjectID || len(admin.Bindings) != 1 {
		t.Errorf("grupo admin deveria vir do AKS e do RBAC, em minúsculas: %+v", admin)
	}
	dev := findGroup(t, got, gidDev)
	if len(dev.Sources) != 1 || dev.Sources[0] != groupSourceRBAC || dev.Bindings[0].Namespace != "app" {
		t.Errorf("grupo dev inesperado: %+v", dev)
	}
	byName := findGroup(t, got, "az_c_squad_x")
	if byName.IsObjectID {
		t.Errorf("subject por nome não é Object ID: %+v", byName)
	}
}

func TestMergeClusterGroupsSemDuplicarSource(t *testing.T) {
	got := mergeClusterGroups([]string{gidAdmin, gidAdmin}, nil, nil)
	if len(got) != 1 || len(got[0].Sources) != 1 {
		t.Errorf("esperava 1 grupo com 1 origem: %+v", got)
	}
}

func TestSortClusterGroupsAdminPrimeiro(t *testing.T) {
	gs := []ClusterGroup{
		{ID: "a", DisplayName: "AAA", Sources: []string{groupSourceRBAC}},
		{ID: "z", DisplayName: "ZZZ", Sources: []string{groupSourceAKSAdmin}},
		{ID: "b", DisplayName: "bbb", Sources: []string{groupSourceRBAC}},
	}
	sortClusterGroups(gs)
	if gs[0].ID != "z" || gs[1].ID != "a" || gs[2].ID != "b" {
		t.Errorf("ordem inesperada: %+v", gs)
	}
}

func TestListBindingGroupsLeBindingsDoCluster(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "rb", Namespace: "app"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "view"},
			Subjects:   []rbacv1.Subject{groupSubject(gidDev)},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb"},
			RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
			Subjects:   []rbacv1.Subject{groupSubject(gidAdmin)},
		},
	)
	bindings, subjects, warns := listBindingGroups(context.Background(), cs)
	if len(warns) != 0 || len(bindings) != 2 || len(subjects) != 2 {
		t.Fatalf("bindings=%v subjects=%v warns=%v", bindings, subjects, warns)
	}
	got := mergeClusterGroups(nil, bindings, subjects)
	if len(got) != 2 {
		t.Errorf("esperava 2 grupos: %+v", got)
	}
}

func TestBindingRisksGruposAbertos(t *testing.T) {
	bindings := []ClusterGroupBinding{
		{Kind: "ClusterRoleBinding", Name: "system:public-info-viewer", RoleKind: "ClusterRole", RoleName: "system:public-info-viewer"},
		{Kind: "ClusterRoleBinding", Name: "oops", RoleKind: "ClusterRole", RoleName: "view"},
		{Kind: "ClusterRoleBinding", Name: "basic", RoleKind: "ClusterRole", RoleName: "system:basic-user"},
		{Kind: "RoleBinding", Name: "anon", Namespace: "app", RoleKind: "Role", RoleName: "editor"},
		{Kind: "ClusterRoleBinding", Name: "masters", RoleKind: "ClusterRole", RoleName: "cluster-admin"},
	}
	subjects := [][]rbacv1.Subject{
		{groupSubject("system:unauthenticated"), groupSubject("system:authenticated")},
		{groupSubject("system:unauthenticated")},
		{groupSubject("system:authenticated")},
		{{Kind: rbacv1.UserKind, Name: "system:anonymous"}},
		{groupSubject("system:masters")},
	}
	got := mergeClusterGroups(nil, bindings, subjects)

	unauth := findGroup(t, got, "system:unauthenticated")
	if !unauth.Builtin || len(unauth.Risks) != 1 {
		t.Errorf("unauthenticated: só a binding 'oops' é fora do padrão: %+v", unauth)
	}
	auth := findGroup(t, got, "system:authenticated")
	if len(auth.Risks) != 0 {
		t.Errorf("authenticated com roles padrão não tem risco: %+v", auth)
	}
	anon := findGroup(t, got, "system:anonymous")
	if len(anon.Risks) != 1 {
		t.Errorf("system:anonymous com Role 'editor' deveria ser risco: %+v", anon)
	}
	masters := findGroup(t, got, "system:masters")
	if !masters.Builtin || len(masters.Risks) != 0 {
		t.Errorf("system:masters é interno e não entra na checagem de grupos abertos: %+v", masters)
	}
}
