package finops

import (
	"context"
	"strings"
	"sync"
	"testing"

	"k8s-hpa-manager/internal/cloudprovider/azure"
)

// Todos os contexts AKS reais do kubeconfig (nomes verbatim) → RG de dados pela convenção.
func TestDeriveDataResourceGroupFromCluster_RealContexts(t *testing.T) {
	cases := map[string]string{
		"akspriv-abastecimento-hlg-admin":    "rg-abastecimento-data-hlg",
		"akspriv-abastecimento-prd-admin":    "rg-abastecimento-data-prd",
		"akspriv-mktplacecarrinho-prd-admin": "rg-mktplacecarrinho-data-prd",
		"akspriv-ofertalogistica-hlg-admin":  "rg-ofertalogistica-data-hlg",
		"akspriv-oferta-prd":                 "rg-oferta-data-prd", // sem -admin
		"AKSPRIV-TMS-HLG-ADMIN":              "rg-tms-data-hlg",    // caixa não importa
		"akspub-loja-prd-admin":              "rg-loja-data-prd",   // outro prefixo aks*
		"akspriv-a-b-c-hlg-admin":            "rg-a-b-c-data-hlg",  // nome com hífen: só o env final sai
	}
	for in, want := range cases {
		if got, ok := DeriveDataResourceGroupFromCluster(in); !ok || got != want {
			t.Errorf("%s → %q,%v; quer %q", in, got, ok, want)
		}
	}
	// Não-AKS ou fora da convenção: sem derivação (nunca inventa nome).
	for _, in := range []string{"asaplog-production-admin", "cluster-apis-prd", "asapops-admin", "arn:aws:eks:us-east-1:1:cluster/x", "gke_proj_us_x", "akspriv-semenv-admin", ""} {
		if got, ok := DeriveDataResourceGroupFromCluster(in); ok {
			t.Errorf("%q não deveria derivar, veio %q", in, got)
		}
	}
}

func TestDataRGCandidates_NameFirstThenConfigRGWithoutDuplicates(t *testing.T) {
	// Igual nos dois: uma só tentativa.
	if got := DataRGCandidates("akspriv-tms-hlg-admin", "rg-tms-app-hlg"); len(got) != 1 || got[0] != "rg-tms-data-hlg" {
		t.Errorf("mesmo nome: %v", got)
	}
	// Cluster cujo RG não tem o nome do cluster: nome do cluster primeiro, RG do config depois.
	got := DataRGCandidates("akspriv-mktplacecarrinho-prd-admin", "rg-mktplace-app-prd")
	if len(got) != 2 || got[0] != "rg-mktplacecarrinho-data-prd" || got[1] != "rg-mktplace-data-prd" {
		t.Errorf("mktplacecarrinho: %v", got)
	}
	// Só no kubeconfig (sem entrada no config): só o nome do cluster.
	if got := DataRGCandidates("akspriv-wms-prd-admin", ""); len(got) != 1 || got[0] != "rg-wms-data-prd" {
		t.Errorf("sem config: %v", got)
	}
	// RG do config em MAIÚSCULAS ("RG-X-APP-HLG") também deriva.
	if got := DataRGCandidates("nao-aks", "RG-BLKHAWK-APP-PRD"); len(got) != 1 || got[0] != "rg-blkhawk-data-prd" {
		t.Errorf("RG em maiúsculas: %v", got)
	}
}

type fakeProber struct {
	subs   []azure.Subscription
	exists map[string]bool // "subID|rg(lower)"
	mu     sync.Mutex
	calls  int
}

func (f *fakeProber) ListSubscriptions(context.Context) ([]azure.Subscription, error) {
	return f.subs, nil
}
func (f *fakeProber) RGExists(_ context.Context, sub, rg string) (bool, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.exists[sub+"|"+strings.ToLower(rg)], nil
}

func subs3() []azure.Subscription {
	return []azure.Subscription{{ID: "s-back", Name: "DEV/HLG - BACKOFFICE"}, {ID: "s-online", Name: "DEV/HLG - ONLINE"}, {ID: "s-prd", Name: "PRD - ONLINE"}}
}

func TestResolveDataRG_FastPathInClusterSubscription(t *testing.T) {
	p := &fakeProber{subs: subs3(), exists: map[string]bool{"s-back|rg-tracking-data-hlg": true}}
	r, err := ResolveDataResourceGroup(context.Background(), p, []string{"rg-tracking-data-hlg"}, "DEV/HLG - BACKOFFICE")
	if err != nil || r.SubscriptionID != "s-back" || !r.InClusterSubscription {
		t.Fatalf("%+v %v", r, err)
	}
	if len(r.OtherSubscriptions) != 0 {
		t.Errorf("nome só existe na subscription do cluster: %v", r.OtherSubscriptions)
	}
}

// Caso real: cshub-hlg roda em BACKOFFICE e o RG de dados está em ONLINE.
func TestResolveDataRG_FoundInAnotherSubscription(t *testing.T) {
	p := &fakeProber{subs: subs3(), exists: map[string]bool{"s-online|rg-cshub-data-hlg": true}}
	r, err := ResolveDataResourceGroup(context.Background(), p, []string{"rg-cshub-data-hlg"}, "s-back") // config guarda o UUID
	if err != nil || r.SubscriptionID != "s-online" || r.InClusterSubscription || r.SubscriptionName != "DEV/HLG - ONLINE" {
		t.Fatalf("%+v %v", r, err)
	}
}

// Caso real: mktplacecarrinho — o nome do cluster não bate, o RG do config (outro nome, outra sub) sim.
func TestResolveDataRG_FallsBackToSecondCandidate(t *testing.T) {
	p := &fakeProber{subs: subs3(), exists: map[string]bool{"s-prd|rg-mktplace-data-prd": true}}
	r, err := ResolveDataResourceGroup(context.Background(), p, []string{"rg-mktplacecarrinho-data-prd", "rg-mktplace-data-prd"}, "s-online")
	if err != nil || r.ResourceGroup != "rg-mktplace-data-prd" || r.SubscriptionID != "s-prd" {
		t.Fatalf("%+v %v", r, err)
	}
}

// Caso real: o mesmo nome existe em 2 subscriptions (envvias-prd, oferta-prd) → vale a do cluster.
func TestResolveDataRG_SameNameInTwoSubsPrefersCluster(t *testing.T) {
	p := &fakeProber{subs: subs3(), exists: map[string]bool{"s-online|rg-oferta-data-prd": true, "s-prd|rg-oferta-data-prd": true}}
	r, err := ResolveDataResourceGroup(context.Background(), p, []string{"rg-oferta-data-prd"}, "PRD - ONLINE")
	if err != nil || r.SubscriptionID != "s-prd" || !r.InClusterSubscription {
		t.Fatalf("%+v %v", r, err)
	}
	// O RG homônimo da outra subscription é avisado, não escolhido nem somado.
	if len(r.OtherSubscriptions) != 1 || r.OtherSubscriptions[0] != "DEV/HLG - ONLINE" {
		t.Errorf("deveria avisar o RG homônimo: %v", r.OtherSubscriptions)
	}
}

func TestResolveDataRG_AmbiguousAndNotFound(t *testing.T) {
	// existe em 2 subs e nenhuma é a do cluster: ambíguo, nunca um palpite.
	p := &fakeProber{subs: subs3(), exists: map[string]bool{"s-online|rg-x-data-hlg": true, "s-prd|rg-x-data-hlg": true}}
	if _, err := ResolveDataResourceGroup(context.Background(), p, []string{"rg-x-data-hlg"}, "s-back"); err == nil || !strings.Contains(err.Error(), "várias subscriptions") {
		t.Errorf("esperava erro de ambiguidade: %v", err)
	}
	// não existe em lugar nenhum (caso real: marketplace-hlg): erro tipado com o que foi tentado.
	q := &fakeProber{subs: subs3(), exists: map[string]bool{}}
	_, err := ResolveDataResourceGroup(context.Background(), q, []string{"rg-marketplace-data-hlg"}, "s-online")
	nf, ok := err.(*ErrDataRGNotFound)
	if !ok || len(nf.Tried) != 1 || nf.Subscription != 3 {
		t.Errorf("esperava ErrDataRGNotFound: %#v", err)
	}
	if _, err := ResolveDataResourceGroup(context.Background(), q, nil, ""); err == nil {
		t.Error("sem candidatos deveria dar erro")
	}
}
