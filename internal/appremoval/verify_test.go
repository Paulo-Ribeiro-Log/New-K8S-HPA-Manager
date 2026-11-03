package appremoval

import (
	"testing"
	"time"
)

// scanDe devolve o scan original da fixture sem Helm, com UIDs distintos.
func scanDe(t *testing.T) (*Result, []Resource) {
	t.Helper()
	rs := plainFixture()
	for i := range rs {
		rs[i].UID = "uid-" + rs[i].Kind + "-" + rs[i].Name
	}
	res, err := Scan("ns", "frete", rs)
	if err != nil {
		t.Fatal(err)
	}
	return res, rs
}

func without(rs []Resource, drop map[string]bool) []Resource {
	var out []Resource
	for _, r := range rs {
		if !drop[key(r.Kind, r.Name)] {
			out = append(out, r)
		}
	}
	return out
}

func stateOf(t *testing.T, v *VerifyResult, kind, name string) string {
	t.Helper()
	for _, it := range v.Items {
		if it.Kind == kind && it.Name == name {
			return it.State
		}
	}
	t.Fatalf("item %s/%s ausente da verificação", kind, name)
	return ""
}

func TestVerifyTudoRemovido(t *testing.T) {
	orig, rs := scanDe(t)
	drop := map[string]bool{}
	for _, it := range orig.Items {
		drop[key(it.Kind, it.Name)] = true
	}
	v := Verify(orig, without(rs, drop))
	if !v.AppGone || v.Leftovers != nil {
		t.Fatalf("esperava aplicação removida: %+v", v)
	}
	for _, it := range v.Items {
		if it.State != StateRemoved {
			t.Errorf("%s/%s = %s", it.Kind, it.Name, it.State)
		}
	}
}

func TestVerifySobrasTerminatingERecriado(t *testing.T) {
	orig, rs := scanDe(t)
	drop := map[string]bool{}
	for _, it := range orig.Items {
		drop[key(it.Kind, it.Name)] = true
	}
	// o PVC sobrou, o ConfigMap está em Terminating e o Service voltou com outro UID
	keep := without(rs, drop)
	for _, r := range rs {
		switch key(r.Kind, r.Name) {
		case "PersistentVolumeClaim/frete-data":
			keep = append(keep, r)
		case "ConfigMap/frete-cfg":
			r.DeletionTimestamp = "2026-11-03T10:00:00Z"
			r.Finalizers = []string{"example.com/protect"}
			keep = append(keep, r)
		case "Service/frete":
			r.UID = "outro-uid"
			keep = append(keep, r)
		}
	}
	v := Verify(orig, keep)
	if v.AppGone || v.Leftovers == nil {
		t.Fatal("esperava sobras")
	}
	if got := stateOf(t, v, KindPVC, "frete-data"); got != StateRemaining {
		t.Errorf("PVC = %s", got)
	}
	if got := stateOf(t, v, KindConfigMap, "frete-cfg"); got != StateTerminating {
		t.Errorf("ConfigMap = %s", got)
	}
	if got := stateOf(t, v, KindService, "frete"); got != StateRecreated {
		t.Errorf("Service = %s", got)
	}
	if got := stateOf(t, v, KindDeployment, "frete"); got != StateRemoved {
		t.Errorf("Deployment = %s", got)
	}
	if !v.Settling() {
		t.Error("com item Terminating, Settling deve ser true")
	}
	if len(v.Leftovers.Items) != 3 || len(v.Leftovers.Warnings) != 2 {
		t.Errorf("sobras = %d itens, %d avisos", len(v.Leftovers.Items), len(v.Leftovers.Warnings))
	}
	for _, it := range v.Leftovers.Items {
		if it.Kind == KindService && it.UID != "outro-uid" {
			t.Errorf("o recriado deve carregar o UID atual: %s", it.UID)
		}
		if it.Kind == KindPVC && it.DefaultSelected {
			t.Error("PVC continua desmarcado por padrão")
		}
	}
}

func TestVerifyItemNovoDepoisDaRemocao(t *testing.T) {
	orig, rs := scanDe(t)
	drop := map[string]bool{}
	for _, it := range orig.Items {
		drop[key(it.Kind, it.Name)] = true
	}
	// o Deployment voltou (operador) e trouxe um HPA que não existia no scan original
	rest := without(rs, drop)
	for _, r := range rs {
		if r.Kind == KindDeployment && r.Name == "frete" {
			r.UID = "novo-uid"
			rest = append(rest, r)
		}
	}
	rest = append(rest, Resource{Kind: KindHPA, Name: "frete-novo", UID: "u-hpa", Spec: spec(map[string]any{
		"scaleTargetRef": map[string]any{"kind": "Deployment", "name": "frete"}})})
	v := Verify(orig, rest)
	if v.AppGone {
		t.Fatal("esperava sobras")
	}
	var fresh *Item
	for i := range v.Leftovers.Items {
		if v.Leftovers.Items[i].Name == "frete-novo" {
			fresh = &v.Leftovers.Items[i]
		}
	}
	if fresh == nil || fresh.State != StateNew {
		t.Errorf("HPA novo não detectado: %+v", v.Leftovers.Items)
	}
}

func TestVerifyHelmReleaseRemovidoLiberaItensRetidos(t *testing.T) {
	orig, err := Scan("ns", "api", helmFixture())
	if err != nil {
		t.Fatal(err)
	}
	// o release sumiu (sem Secrets sh.helm.release), mas o Service continua (finalizer travado)
	var rs []Resource
	for _, r := range helmFixture() {
		if r.Kind == KindService && r.Name == "api-svc" {
			rs = append(rs, r)
		}
	}
	v := Verify(orig, rs)
	if v.AppGone || v.Leftovers == nil || v.Leftovers.Helm != nil {
		t.Fatalf("resultado: %+v", v)
	}
	if got := stateOf(t, v, KindHelmRelease, "api"); got != StateRemoved {
		t.Errorf("release = %s", got)
	}
	svc := v.Leftovers.Items[0]
	if svc.Name != "api-svc" || svc.RemovedByHelm || !svc.DefaultSelected {
		t.Errorf("com o release removido, o Service sobrando deve ser selecionável: %+v", svc)
	}
}

func TestVerifyHelmRecursoAnotadoSemWorkload(t *testing.T) {
	orig, err := Scan("ns", "api", helmFixture())
	if err != nil {
		t.Fatal(err)
	}
	rs := []Resource{{Kind: KindConfigMap, Name: "api-extra", UID: "u1", Annotations: helmAnn("api")}}
	v := Verify(orig, rs)
	if v.AppGone || len(v.Leftovers.Items) != 1 || v.Leftovers.Items[0].State != StateNew {
		t.Errorf("recurso anotado com o release deve aparecer como novo: %+v", v.Leftovers)
	}
}

func TestStore(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	r := &Result{Name: "x"}
	id := s.Put("c", r)
	if r.ScanID != id {
		t.Error("Put deve gravar o ScanID no resultado")
	}
	cluster, got, ok := s.Get(id)
	if !ok || cluster != "c" || got != r {
		t.Fatalf("get: %v %v %v", cluster, got, ok)
	}
	now = now.Add(storeTTL + time.Minute)
	if _, _, ok := s.Get(id); ok {
		t.Error("entrada expirada não deve ser devolvida")
	}
	if _, _, ok := s.Get("nao-existe"); ok {
		t.Error("id inexistente")
	}
}
