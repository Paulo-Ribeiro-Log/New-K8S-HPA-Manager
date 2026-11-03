package appremoval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	uids      map[string]string // key -> uid atual; ausente = não existe
	failOn    string            // key que falha no Delete
	calls     []string
	helmFails bool
}

func (f *fakeRunner) CurrentUID(_ context.Context, _, _, kind, name string) (string, error) {
	uid, ok := f.uids[key(kind, name)]
	if !ok {
		return "", ErrNotFoundInCluster
	}
	return uid, nil
}

func (f *fakeRunner) Delete(_ context.Context, _, _, kind, name string, dry bool) error {
	f.calls = append(f.calls, fmt.Sprintf("delete %s/%s dry=%v", kind, name, dry))
	if key(kind, name) == f.failOn {
		return errors.New("admission webhook denied")
	}
	return nil
}

func (f *fakeRunner) HelmUninstall(_ context.Context, _, _, release string, dry bool) error {
	f.calls = append(f.calls, fmt.Sprintf("helm uninstall %s dry=%v", release, dry))
	if f.helmFails {
		return errors.New("helm falhou")
	}
	return nil
}

func scanFixture() *Result {
	return &Result{Items: []Item{
		{Kind: KindHelmRelease, Name: "api"},
		{Kind: KindIngress, Name: "ing", UID: "u-ing"},
		{Kind: KindDeployment, Name: "dep", UID: "u-dep", RemovedByHelm: true},
		{Kind: KindService, Name: "svc", UID: "u-svc"},
		{Kind: KindConfigMap, Name: "cfg", UID: "u-cfg"},
		{Kind: "Pod", Name: "x", UID: "u-x"},
	}}
}

func TestPlanValidaContraOScan(t *testing.T) {
	scan := scanFixture()
	cases := []struct {
		name string
		in   []Target
		want error
	}{
		{"vazio", nil, ErrInvalidTarget},
		{"fora do scan", []Target{{Kind: KindService, Name: "outro"}}, ErrInvalidTarget},
		{"coberto pelo helm", []Target{{Kind: KindDeployment, Name: "dep", UID: "u-dep"}}, ErrInvalidTarget},
		{"tipo não removível", []Target{{Kind: "Pod", Name: "x"}}, ErrInvalidTarget},
		{"uid diferente", []Target{{Kind: KindService, Name: "svc", UID: "outro-uid"}}, ErrStale},
	}
	for _, c := range cases {
		if _, err := Plan(scan, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: erro = %v, quer %v", c.name, err, c.want)
		}
	}
}

func TestPlanMantemOrdemDoScanEDeduplica(t *testing.T) {
	plan, err := Plan(scanFixture(), []Target{
		{Kind: KindConfigMap, Name: "cfg", UID: "u-cfg"},
		{Kind: KindHelmRelease, Name: "api"},
		{Kind: KindService, Name: "svc", UID: "u-svc"},
		{Kind: KindService, Name: "svc", UID: "u-svc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range plan {
		got = append(got, p.Kind+"/"+p.Name)
	}
	if strings.Join(got, ",") != "HelmRelease/api,Service/svc,ConfigMap/cfg" {
		t.Errorf("plano = %v", got)
	}
}

func TestRunExecutaEmOrdemEReportaProgresso(t *testing.T) {
	r := &fakeRunner{uids: map[string]string{"Ingress/ing": "u-ing", "Service/svc": "u-svc"}}
	plan := []Target{{Kind: KindIngress, Name: "ing", UID: "u-ing"}, {Kind: KindService, Name: "svc", UID: "u-svc"}}
	var dones []int
	out := Run(context.Background(), r, "c", "ns", plan, false, func(_ Outcome, done, total int) {
		dones = append(dones, done*10+total)
	})
	if len(out) != 2 || out[0].Status != StatusDeleted || out[1].Status != StatusDeleted {
		t.Fatalf("outcomes = %+v", out)
	}
	if len(dones) != 2 || dones[0] != 12 || dones[1] != 22 {
		t.Errorf("progresso = %v", dones)
	}
}

func TestRunDryRunNaoApaga(t *testing.T) {
	r := &fakeRunner{uids: map[string]string{"Service/svc": "u-svc"}}
	out := Run(context.Background(), r, "c", "ns", []Target{{Kind: KindService, Name: "svc", UID: "u-svc"}}, true, nil)
	if out[0].Status != StatusSimulated || r.calls[0] != "delete Service/svc dry=true" {
		t.Errorf("out=%+v calls=%v", out, r.calls)
	}
}

func TestRunUIDDiferenteNaoApaga(t *testing.T) {
	r := &fakeRunner{uids: map[string]string{"Service/svc": "recriado"}}
	out := Run(context.Background(), r, "c", "ns", []Target{{Kind: KindService, Name: "svc", UID: "u-svc"}}, false, nil)
	if out[0].Status != StatusSkipped || len(r.calls) != 0 {
		t.Errorf("out=%+v calls=%v", out, r.calls)
	}
}

func TestRunJaRemovidoContinua(t *testing.T) {
	r := &fakeRunner{uids: map[string]string{"Service/svc": "u-svc"}}
	plan := []Target{{Kind: KindIngress, Name: "ing", UID: "u-ing"}, {Kind: KindService, Name: "svc", UID: "u-svc"}}
	out := Run(context.Background(), r, "c", "ns", plan, false, nil)
	if out[0].Status != StatusNotFound || out[1].Status != StatusDeleted {
		t.Errorf("out=%+v", out)
	}
}

func TestRunParaNaPrimeiraFalha(t *testing.T) {
	r := &fakeRunner{
		uids:   map[string]string{"Ingress/ing": "u-ing", "Service/svc": "u-svc", "ConfigMap/cfg": "u-cfg"},
		failOn: "Service/svc",
	}
	plan := []Target{
		{Kind: KindIngress, Name: "ing", UID: "u-ing"},
		{Kind: KindService, Name: "svc", UID: "u-svc"},
		{Kind: KindConfigMap, Name: "cfg", UID: "u-cfg"},
	}
	out := Run(context.Background(), r, "c", "ns", plan, false, nil)
	if out[1].Status != StatusFailed || !strings.Contains(out[1].Message, "admission") {
		t.Errorf("falha: %+v", out[1])
	}
	if out[2].Status != StatusSkipped || len(r.calls) != 2 {
		t.Errorf("restante deveria ser pulado: out=%+v calls=%v", out, r.calls)
	}
}

func TestRunHelmFalhaPulaOResto(t *testing.T) {
	r := &fakeRunner{helmFails: true, uids: map[string]string{"Service/svc": "u-svc"}}
	plan := []Target{{Kind: KindHelmRelease, Name: "api"}, {Kind: KindService, Name: "svc", UID: "u-svc"}}
	out := Run(context.Background(), r, "c", "ns", plan, false, nil)
	if out[0].Status != StatusFailed || out[1].Status != StatusSkipped {
		t.Errorf("out=%+v", out)
	}
}

func TestRunContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fakeRunner{}
	out := Run(ctx, r, "c", "ns", []Target{{Kind: KindService, Name: "svc"}}, false, nil)
	if out[0].Status != StatusSkipped || len(r.calls) != 0 {
		t.Errorf("out=%+v", out)
	}
}

func TestKubectlRunnerArgs(t *testing.T) {
	orig := kubectlRun
	defer func() { kubectlRun = orig }()
	var got [][]string
	kubectlRun = func(_ context.Context, args ...string) ([]byte, error) {
		got = append(got, args)
		return []byte("uid-1"), nil
	}
	k := KubectlRunner{}
	uid, err := k.CurrentUID(context.Background(), "ctx", "ns", KindDeployment, "dep")
	if err != nil || uid != "uid-1" {
		t.Fatalf("uid=%q err=%v", uid, err)
	}
	if err := k.Delete(context.Background(), "ctx", "ns", KindDeployment, "dep", true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got[0], " ") != "get deployments.apps dep -n ns --context ctx -o jsonpath={.metadata.uid}" {
		t.Errorf("get: %v", got[0])
	}
	if strings.Join(got[1], " ") != "delete deployments.apps dep -n ns --context ctx --timeout=150s --cascade=foreground --dry-run=server" {
		t.Errorf("delete: %v", got[1])
	}
	if err := k.Delete(context.Background(), "ctx", "ns", KindService, "svc", false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got[2], " ") != "delete services svc -n ns --context ctx --timeout=90s" {
		t.Errorf("delete de Service não usa cascade foreground: %v", got[2])
	}
	if err := k.Delete(context.Background(), "ctx", "ns", "Pod", "x", false); !errors.Is(err, ErrInvalidTarget) {
		t.Errorf("tipo fora da lista deve ser recusado: %v", err)
	}
}

func TestKubectlRunnerNotFound(t *testing.T) {
	orig := kubectlRun
	defer func() { kubectlRun = orig }()
	kubectlRun = func(context.Context, ...string) ([]byte, error) {
		return nil, errors.New(`Error from server (NotFound): services "x" not found`)
	}
	k := KubectlRunner{}
	if _, err := k.CurrentUID(context.Background(), "c", "ns", KindService, "x"); !errors.Is(err, ErrNotFoundInCluster) {
		t.Errorf("get: %v", err)
	}
	if err := k.Delete(context.Background(), "c", "ns", KindService, "x", false); err != nil {
		t.Errorf("delete de recurso ausente deve ser tolerado: %v", err)
	}
}
