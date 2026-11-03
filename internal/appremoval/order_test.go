package appremoval

import "testing"

func pos(items []Item, kind, name string) int {
	for i, it := range items {
		if it.Kind == kind && it.Name == name {
			return i
		}
	}
	return -1
}

func before(t *testing.T, items []Item, a, b [2]string) {
	t.Helper()
	pa, pb := pos(items, a[0], a[1]), pos(items, b[0], b[1])
	if pa < 0 || pb < 0 {
		t.Fatalf("item ausente: %v=%d %v=%d", a, pa, b, pb)
	}
	if pa >= pb {
		t.Errorf("%s/%s (pos %d) deve vir antes de %s/%s (pos %d)", a[0], a[1], pa, b[0], b[1], pb)
	}
}

// Quem recria vem antes do que é recriado.
func TestOrdemDeRemocaoEvitaRecriacao(t *testing.T) {
	lbl := map[string]string{"app": "a"}
	rs := []Resource{
		{Kind: KindCronJob, Name: "a-cron", Labels: lbl, Annotations: helmAnn("rel"), Spec: spec(map[string]any{"jobTemplate": map[string]any{
			"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"labels": lbl}, "spec": map[string]any{}}}}})},
		{Kind: KindStatefulSet, Name: "a", Labels: lbl, Annotations: helmAnn("rel"), Spec: spec(map[string]any{
			"template": map[string]any{"metadata": map[string]any{"labels": lbl}, "spec": map[string]any{
				"serviceAccountName": "a-sa",
				"containers":         []any{map[string]any{"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "a-sec"}}}}}}},
			"volumeClaimTemplates": []any{map[string]any{"metadata": map[string]any{"name": "data"}}}})},
		{Kind: KindDaemonSet, Name: "a-ds", Labels: lbl, Annotations: helmAnn("rel"), Spec: spec(podTpl(lbl, map[string]any{}))},
		{Kind: KindHPA, Name: "a-hpa", Spec: spec(map[string]any{"scaleTargetRef": map[string]any{"kind": "StatefulSet", "name": "a"}})},
		{Kind: KindService, Name: "a-svc", Spec: spec(map[string]any{"selector": lbl})},
		{Kind: KindIngress, Name: "a-ing", Spec: spec(map[string]any{"rules": []any{map[string]any{"http": map[string]any{
			"paths": []any{map[string]any{"backend": map[string]any{"service": map[string]any{"name": "a-svc"}}}}}}}})},
		{Kind: KindPVC, Name: "data-a-0"},
		{Kind: KindSecret, Name: "a-sec", Owners: []Owner{{Kind: KindExternalSecret, Name: "a-es", UID: "u"}}},
		{Kind: KindSA, Name: "a-sa"},
		{Kind: KindRoleBinding, Name: "a-rb", RoleRefKind: "Role", RoleRefName: "a-role",
			Subjects: []Subject{{Kind: "ServiceAccount", Name: "a-sa"}}},
		{Kind: KindRole, Name: "a-role"},
	}
	res, err := Scan("ns", "a", rs)
	if err != nil {
		t.Fatal(err)
	}
	it := res.Items
	if it[0].Kind != KindHelmRelease {
		t.Errorf("o release Helm é a primeira etapa: %s", it[0].Kind)
	}
	before(t, it, [2]string{KindIngress, "a-ing"}, [2]string{KindHPA, "a-hpa"})
	before(t, it, [2]string{KindHPA, "a-hpa"}, [2]string{KindCronJob, "a-cron"})
	before(t, it, [2]string{KindCronJob, "a-cron"}, [2]string{KindStatefulSet, "a"})
	before(t, it, [2]string{KindCronJob, "a-cron"}, [2]string{KindDaemonSet, "a-ds"})
	before(t, it, [2]string{KindStatefulSet, "a"}, [2]string{KindService, "a-svc"})
	before(t, it, [2]string{KindService, "a-svc"}, [2]string{KindExternalSecret, "a-es"})
	before(t, it, [2]string{KindExternalSecret, "a-es"}, [2]string{KindSecret, "a-sec"}) // senão o Secret volta
	before(t, it, [2]string{KindStatefulSet, "a"}, [2]string{KindPVC, "data-a-0"})       // senão o StatefulSet recria o PVC
	before(t, it, [2]string{KindSecret, "a-sec"}, [2]string{KindPVC, "data-a-0"})
	before(t, it, [2]string{KindPVC, "data-a-0"}, [2]string{KindRoleBinding, "a-rb"})
	before(t, it, [2]string{KindRoleBinding, "a-rb"}, [2]string{KindRole, "a-role"})
	before(t, it, [2]string{KindRole, "a-role"}, [2]string{KindSA, "a-sa"})
	for _, i := range it {
		if i.Step == "" {
			t.Errorf("%s/%s sem etapa", i.Kind, i.Name)
		}
	}
	if it[pos(it, KindStatefulSet, "a")].Step != it[pos(it, KindDaemonSet, "a-ds")].Step {
		t.Error("StatefulSet e DaemonSet são da mesma etapa (controllers)")
	}
}

func TestStepTitlesNaoSeRepetemEntreEtapas(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range removalPlan {
		if seen[s.Title] {
			t.Errorf("título repetido: %s", s.Title)
		}
		seen[s.Title] = true
	}
	if removalRank("Desconhecido") != len(removalPlan)*10 {
		t.Error("kind desconhecido vai por último")
	}
}

func TestVerifyMantemAOrdemDoPlano(t *testing.T) {
	orig, rs := scanDe(t)
	v := Verify(orig, rs) // nada removido: tudo sobrou
	if v.Leftovers == nil {
		t.Fatal("esperava sobras")
	}
	before(t, v.Leftovers.Items, [2]string{KindIngress, "frete-ing"}, [2]string{KindDeployment, "frete"})
	before(t, v.Leftovers.Items, [2]string{KindDeployment, "frete"}, [2]string{KindPVC, "frete-data"})
}
