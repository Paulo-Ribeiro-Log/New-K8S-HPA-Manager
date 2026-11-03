package appremoval

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func spec(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func podTpl(labels map[string]string, ps map[string]any) map[string]any {
	return map[string]any{"template": map[string]any{
		"metadata": map[string]any{"labels": labels},
		"spec":     ps,
	}}
}

func find(t *testing.T, res *Result, kind, name string) Item {
	t.Helper()
	for _, it := range res.Items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("item %s/%s não encontrado; itens: %+v", kind, name, res.Items)
	return Item{}
}

func absent(t *testing.T, res *Result, kind, name string) {
	t.Helper()
	for _, it := range res.Items {
		if it.Kind == kind && it.Name == name {
			t.Fatalf("item %s/%s não deveria estar no resultado", kind, name)
		}
	}
}

// App sem Helm: Deployment + Service + Ingress + HPA + config, com um Secret e um ConfigMap
// também usados por outro Deployment.
func plainFixture() []Resource {
	lbl := map[string]string{"app": "frete"}
	applied := map[string]string{annLastApplied: "{}"}
	return []Resource{
		{Kind: KindDeployment, Name: "frete", Labels: lbl, Annotations: applied, Spec: spec(podTpl(lbl, map[string]any{
			"serviceAccountName": "frete-sa",
			"containers": []any{map[string]any{
				"envFrom": []any{map[string]any{"configMapRef": map[string]any{"name": "frete-cfg"}}},
				"env": []any{map[string]any{"valueFrom": map[string]any{
					"secretKeyRef": map[string]any{"name": "shared-secret"}}}},
			}},
			"volumes": []any{map[string]any{"persistentVolumeClaim": map[string]any{"claimName": "frete-data"}}},
		}))},
		{Kind: KindDeployment, Name: "outro", Labels: map[string]string{"app": "outro"}, Spec: spec(podTpl(
			map[string]string{"app": "outro"}, map[string]any{
				"containers": []any{map[string]any{
					"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "shared-secret"}}}}},
			}))},
		{Kind: KindService, Name: "frete", Spec: spec(map[string]any{"selector": lbl})},
		{Kind: KindService, Name: "outro", Spec: spec(map[string]any{"selector": map[string]string{"app": "outro"}})},
		{Kind: KindIngress, Name: "frete-ing", Spec: spec(map[string]any{"rules": []any{map[string]any{
			"http": map[string]any{"paths": []any{map[string]any{
				"backend": map[string]any{"service": map[string]any{"name": "frete"}}}}}}}})},
		{Kind: KindIngress, Name: "misto", Spec: spec(map[string]any{"rules": []any{map[string]any{
			"http": map[string]any{"paths": []any{
				map[string]any{"backend": map[string]any{"service": map[string]any{"name": "frete"}}},
				map[string]any{"backend": map[string]any{"service": map[string]any{"name": "outro"}}},
			}}}}})},
		{Kind: KindHPA, Name: "frete", Spec: spec(map[string]any{
			"scaleTargetRef": map[string]any{"kind": "Deployment", "name": "frete"}})},
		{Kind: KindConfigMap, Name: "frete-cfg"},
		{Kind: KindSecret, Name: "shared-secret"},
		{Kind: KindPVC, Name: "frete-data"},
		{Kind: KindSA, Name: "frete-sa"},
		{Kind: KindRoleBinding, Name: "frete-rb", RoleRefKind: "Role", RoleRefName: "frete-role",
			Subjects: []Subject{{Kind: "ServiceAccount", Name: "frete-sa"}}},
		{Kind: KindRole, Name: "frete-role"},
		{Kind: KindConfigMap, Name: "nao-relacionado"},
	}
}

func TestScanSemHelm(t *testing.T) {
	res, err := Scan("ns", "frete", plainFixture())
	if err != nil {
		t.Fatal(err)
	}
	if res.Origin != OriginKustomizeProvavel {
		t.Errorf("origem = %s, quer kustomize-provavel", res.Origin)
	}
	if res.Helm != nil {
		t.Error("não deveria ter Helm")
	}
	for _, c := range [][2]string{
		{KindDeployment, "frete"}, {KindService, "frete"}, {KindIngress, "frete-ing"},
		{KindHPA, "frete"}, {KindConfigMap, "frete-cfg"}, {KindPVC, "frete-data"},
		{KindSA, "frete-sa"}, {KindRoleBinding, "frete-rb"}, {KindRole, "frete-role"},
	} {
		find(t, res, c[0], c[1])
	}
	absent(t, res, KindService, "outro")
	absent(t, res, KindDeployment, "outro")
	absent(t, res, KindConfigMap, "nao-relacionado")

	if !find(t, res, KindDeployment, "frete").DefaultSelected {
		t.Error("workload deveria vir marcado")
	}
	pvc := find(t, res, KindPVC, "frete-data")
	if pvc.DefaultSelected || !pvc.DataRisk {
		t.Errorf("PVC deve vir desmarcado e com risco de dados: %+v", pvc)
	}
	sec := find(t, res, KindSecret, "shared-secret")
	if !sec.Shared || sec.DefaultSelected || len(sec.UsedBy) != 1 || sec.UsedBy[0] != "Deployment/outro" {
		t.Errorf("Secret compartilhado incorreto: %+v", sec)
	}
	mixed := find(t, res, KindIngress, "misto")
	if !mixed.Shared || mixed.DefaultSelected || mixed.UsedBy[0] != "outro" {
		t.Errorf("Ingress com backend de outra app deve ser compartilhado: %+v", mixed)
	}
	if !find(t, res, KindConfigMap, "frete-cfg").DefaultSelected {
		t.Error("ConfigMap exclusivo deve vir marcado")
	}
	if len(res.Warnings) == 0 {
		t.Error("esperava avisos (origem inferida e itens compartilhados)")
	}
	// ordem de remoção: Ingress antes do workload, workload antes do Service
	pos := map[string]int{}
	for i, it := range res.Items {
		pos[it.Kind+"/"+it.Name] = i
	}
	if !(pos["Ingress/frete-ing"] < pos["Deployment/frete"] && pos["Deployment/frete"] < pos["Service/frete"]) {
		t.Errorf("ordem de remoção incorreta: %v", pos)
	}
}

func helmAnn(rel string) map[string]string { return map[string]string{annHelmRelease: rel} }

func helmFixture() []Resource {
	lbl := map[string]string{"app.kubernetes.io/name": "api"}
	keep := helmAnn("api")
	keep[annHelmPolicy] = valueHelmPolicy
	return []Resource{
		{Kind: KindDeployment, Name: "api-deploy", Labels: lbl, Annotations: helmAnn("api"), Spec: spec(podTpl(lbl, map[string]any{
			"containers": []any{map[string]any{}}}))},
		{Kind: KindService, Name: "api-svc", Labels: lbl, Annotations: helmAnn("api"), Spec: spec(map[string]any{"selector": lbl})},
		{Kind: KindSecret, Name: "api-secret", Annotations: helmAnn("api")},
		{Kind: KindPVC, Name: "api-pvc-keep", Annotations: keep},
		{Kind: KindSecret, Name: "sh.helm.release.v1.api.v1", Type: helmSecretType, Labels: map[string]string{"name": "api"}},
		{Kind: KindSecret, Name: "sh.helm.release.v1.api.v2", Type: helmSecretType, Labels: map[string]string{"name": "api"}},
		{Kind: KindServiceMonitor, Name: "api-sm", Spec: spec(map[string]any{
			"selector": map[string]any{"matchLabels": lbl}})},
	}
}

func TestScanHelmPorNomeDoWorkload(t *testing.T) {
	res, err := Scan("ns", "api-deploy", helmFixture())
	if err != nil {
		t.Fatal(err)
	}
	checkHelm(t, res)
}

func TestScanHelmPorNomeDoRelease(t *testing.T) {
	res, err := Scan("ns", "api", helmFixture())
	if err != nil {
		t.Fatal(err)
	}
	checkHelm(t, res)
}

func checkHelm(t *testing.T, res *Result) {
	t.Helper()
	if res.Origin != OriginHelm || res.Helm == nil || res.Helm.Release != "api" || res.Helm.Revisions != 2 {
		t.Fatalf("Helm incorreto: origem=%s helm=%+v", res.Origin, res.Helm)
	}
	rel := find(t, res, KindHelmRelease, "api")
	if !rel.DefaultSelected {
		t.Error("o release deve vir marcado")
	}
	if res.Items[0].Kind != KindHelmRelease {
		t.Error("o release deve ser o primeiro item")
	}
	svc := find(t, res, KindService, "api-svc")
	if !svc.RemovedByHelm || svc.DefaultSelected || svc.Origin != OriginHelm {
		t.Errorf("Service do release deve ser coberto pelo helm: %+v", svc)
	}
	sec := find(t, res, KindSecret, "api-secret")
	if !sec.RemovedByHelm || !sec.DataRisk {
		t.Errorf("Secret do release: %+v", sec)
	}
	pvc := find(t, res, KindPVC, "api-pvc-keep")
	if pvc.RemovedByHelm {
		t.Errorf("PVC com resource-policy keep não é removido pelo helm: %+v", pvc)
	}
	find(t, res, KindServiceMonitor, "api-sm")
	absent(t, res, KindSecret, "sh.helm.release.v1.api.v1")
	if len(res.Warnings) == 0 {
		t.Error("esperava aviso de que o helm uninstall remove o Secret")
	}
}

func TestScanStatefulSetVolumeClaimTemplate(t *testing.T) {
	lbl := map[string]string{"app": "db"}
	rs := []Resource{
		{Kind: KindStatefulSet, Name: "db", Spec: spec(map[string]any{
			"template":             map[string]any{"metadata": map[string]any{"labels": lbl}, "spec": map[string]any{}},
			"volumeClaimTemplates": []any{map[string]any{"metadata": map[string]any{"name": "data"}}},
		})},
		{Kind: KindPVC, Name: "data-db-0"},
		{Kind: KindPVC, Name: "data-db-1"},
		{Kind: KindPVC, Name: "data-outro-0"},
	}
	res, err := Scan("ns", "db", rs)
	if err != nil {
		t.Fatal(err)
	}
	find(t, res, KindPVC, "data-db-0")
	find(t, res, KindPVC, "data-db-1")
	absent(t, res, KindPVC, "data-outro-0")
}

func TestScanExternalSecretDono(t *testing.T) {
	rs := []Resource{
		{Kind: KindDeployment, Name: "app", Spec: spec(podTpl(map[string]string{"app": "a"}, map[string]any{
			"containers": []any{map[string]any{
				"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "app-sec"}}}}}}))},
		{Kind: KindSecret, Name: "app-sec", Owners: []Owner{{Kind: KindExternalSecret, Name: "app-es", UID: "u1"}}},
	}
	res, err := Scan("ns", "app", rs)
	if err != nil {
		t.Fatal(err)
	}
	es := find(t, res, KindExternalSecret, "app-es")
	if es.DefaultSelected || es.UID != "u1" {
		t.Errorf("ExternalSecret: %+v", es)
	}
}

func TestScanNaoEncontrado(t *testing.T) {
	_, err := Scan("ns", "inexistente", plainFixture())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("erro = %v, quer ErrNotFound", err)
	}
}

func TestOriginOf(t *testing.T) {
	cases := []struct {
		name string
		r    Resource
		want Origin
	}{
		{"helm", Resource{Annotations: helmAnn("x")}, OriginHelm},
		{"kustomize", Resource{Labels: map[string]string{labelManagedBy: "kustomize"}}, OriginKustomize},
		{"provável", Resource{Labels: map[string]string{"app": "x"}, Annotations: map[string]string{annLastApplied: "{}"}}, OriginKustomizeProvavel},
		{"manual", Resource{}, OriginManual},
		{"só label", Resource{Labels: map[string]string{"app": "x"}}, OriginManual},
	}
	for _, c := range cases {
		if got := originOf(&c.r); got != c.want {
			t.Errorf("%s: origem = %s, quer %s", c.name, got, c.want)
		}
	}
}

func TestParseListDescartaDadosDeSecret(t *testing.T) {
	raw := `{"items":[{"kind":"Secret","type":"Opaque","metadata":{"name":"s","uid":"1"},"data":{"k":"c2VjcmV0"}},
	{"kind":"RoleBinding","metadata":{"name":"rb"},"subjects":[{"kind":"ServiceAccount","name":"sa"}],"roleRef":{"kind":"Role","name":"r"}}]}`
	rs, err := ParseList([]byte(raw))
	if err != nil || len(rs) != 2 {
		t.Fatalf("err=%v len=%d", err, len(rs))
	}
	if rs[0].Name != "s" || rs[0].Type != "Opaque" || rs[0].Spec != nil {
		t.Errorf("secret: %+v", rs[0])
	}
	if rs[1].RoleRefName != "r" || len(rs[1].Subjects) != 1 {
		t.Errorf("rolebinding: %+v", rs[1])
	}
}

func TestCollectIgnoraCRDAusente(t *testing.T) {
	orig := kubectlGet
	defer func() { kubectlGet = orig }()
	kubectlGet = func(_ context.Context, _, _, kinds string) ([]byte, error) {
		if kinds == coreKinds {
			return []byte(`{"items":[{"kind":"Deployment","metadata":{"name":"a"}}]}`), nil
		}
		return nil, errors.New(`the server doesn't have a resource type`)
	}
	rs, err := Collect(context.Background(), "c", "ns")
	if err != nil || len(rs) != 1 {
		t.Fatalf("err=%v rs=%v", err, rs)
	}
}

func TestCollectFalhaNaListagemBasica(t *testing.T) {
	orig := kubectlGet
	defer func() { kubectlGet = orig }()
	kubectlGet = func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("forbidden")
	}
	if _, err := Collect(context.Background(), "c", "ns"); err == nil {
		t.Fatal("esperava erro")
	}
}

func TestCandidates(t *testing.T) {
	rs := []Resource{
		{Kind: KindDeployment, Name: "b", Annotations: helmAnn("rel")},
		{Kind: KindCronJob, Name: "a"},
	}
	got := candidatesFrom(rs)
	if len(got) != 3 || got[0].Name != "a" || got[2].Kind != KindHelmRelease {
		t.Errorf("candidatos: %+v", got)
	}
}
