package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/models"
)

// argFake responde às consultas do Resource Graph com as linhas de `rows` e guarda as queries.
type argFake struct {
	rows    []map[string]any
	queries []string
}

func (f *argFake) RoundTrip(req *http.Request) (*http.Response, error) {
	var body struct {
		Query string `json:"query"`
	}
	b, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(b, &body)
	f.queries = append(f.queries, body.Query)
	out, _ := json.Marshal(map[string]any{"data": f.rows})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(out))), Header: http.Header{}}, nil
}

func fakeARM(f *argFake) *ARMClient {
	return &ARMClient{token: "t", http: &http.Client{Transport: f}}
}

func TestKQLList(t *testing.T) {
	if got := kqlList([]string{"rg-a", "it's"}); got != `('rg-a', 'it\'s')` {
		t.Errorf("kqlList = %s", got)
	}
	if got := kqlList(nil); got != "('__nenhum__')" {
		t.Errorf("kqlList vazio = %s", got)
	}
}

func TestResolveJourneyResourceGroups(t *testing.T) {
	f := &argFake{rows: []map[string]any{
		{"subscriptionId": "s1", "resourceGroup": "rg-logistica-shared", "journey": "logistica", "clusterName": "", "source": "rg"},
		{"subscriptionId": "s1", "resourceGroup": "rg-entregas-app-prd", "journey": "", "clusterName": "", "source": "rg"},
		{"subscriptionId": "s2", "resourceGroup": "rg-entregas-data-prd", "journey": "", "clusterName": "", "source": "rg"},
		{"subscriptionId": "s1", "resourceGroup": "MC_rg-entregas-app-prd_aks-entregas-prd_brazilsouth", "journey": "", "clusterName": "aks-entregas-prd", "source": "node"},
		// Mesmo RG de app voltando pela tag: não duplica e mantém a origem "cluster".
		{"subscriptionId": "s1", "resourceGroup": "rg-entregas-app-prd", "journey": "logistica", "clusterName": "", "source": "rg"},
	}}
	clusters := []JourneyCluster{{
		Name: "aks-entregas-prd", Journey: "logistica", AppResourceGroup: "rg-entregas-app-prd",
		DataRGCandidates: []string{"rg-entregas-data-prd"},
	}}
	rgs, err := ResolveJourneyResourceGroups(context.Background(), fakeARM(f), []string{"s1", "s2"}, []string{"Logistica"}, clusters)
	if err != nil {
		t.Fatal(err)
	}
	if len(rgs) != 4 {
		t.Fatalf("esperava 4 RGs (sem duplicata), got %+v", rgs)
	}
	src := map[string]string{}
	for _, rg := range rgs {
		src[rg.ResourceGroup] = rg.Source
		if rg.Journey != "logistica" {
			t.Errorf("%s: jornada %q, esperava logistica", rg.ResourceGroup, rg.Journey)
		}
	}
	want := map[string]string{
		"rg-logistica-shared": "tag", "rg-entregas-app-prd": "cluster", "rg-entregas-data-prd": "data",
		"MC_rg-entregas-app-prd_aks-entregas-prd_brazilsouth": "node",
	}
	for rg, s := range want {
		if src[rg] != s {
			t.Errorf("%s: origem %q, esperava %q", rg, src[rg], s)
		}
	}
	q := f.queries[0]
	for _, part := range []string{"tolower(journey) in ('logistica')", "'rg-entregas-data-prd'", "'aks-entregas-prd'", "nodeResourceGroup"} {
		if !strings.Contains(q, part) {
			t.Errorf("query não contém %q:\n%s", part, q)
		}
	}
}

func TestListOrphanResources(t *testing.T) {
	mc := "MC_rg-entregas-app-prd_aks-entregas-prd_brazilsouth"
	f := &argFake{rows: []map[string]any{
		{
			"id": "/subscriptions/s1/resourceGroups/" + mc + "/providers/Microsoft.Compute/disks/pvc-1", "name": "pvc-1",
			"type": "microsoft.compute/disks", "location": "brazilsouth", "resourceGroup": mc, "subscriptionId": "s1",
			"tags":       map[string]string{"kubernetes.io-created-for-pvc-name": "data-mongo-0"},
			"sku":        map[string]string{"name": "Premium_LRS"},
			"reason":     "Disco não atachado a nenhuma VM",
			"properties": map[string]any{"diskState": "Unattached", "diskSizeGB": 128, "LastOwnershipUpdateTime": "2026-09-01T10:00:00Z"},
		},
		{
			"id": "/subscriptions/s1/resourceGroups/rg-logistica-shared/providers/Microsoft.Network/publicIPAddresses/pip-velho", "name": "pip-velho",
			"type": "microsoft.network/publicipaddresses", "resourceGroup": "rg-logistica-shared", "subscriptionId": "s1",
			"sku": map[string]string{"name": "Standard", "tier": "Regional"}, "reason": "IP público sem associação",
		},
	}}
	rgs := []models.ScopedResourceGroup{
		{SubscriptionID: "s1", ResourceGroup: mc, Journey: "logistica", Source: "node"},
		{SubscriptionID: "s1", ResourceGroup: "rg-logistica-shared", Journey: "logistica", Source: "tag"},
	}
	disks, others, err := ListOrphanResources(context.Background(), fakeARM(f), []string{"s1"}, rgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 || disks[0].K8sClusterHint != mc || disks[0].K8sPVCName != "data-mongo-0" ||
		disks[0].UnattachedSince != "2026-09-01T10:00:00Z" || disks[0].SizeGB != 128 || disks[0].Journey != "logistica" {
		t.Errorf("disco convertido errado: %+v", disks)
	}
	if len(others) != 1 || others[0].Reason != "IP público sem associação" || others[0].SKU != "Standard Regional" || others[0].Journey != "logistica" {
		t.Errorf("órfão convertido errado: %+v", others)
	}
	if !strings.Contains(f.queries[0], strings.ToLower("s1/"+mc)) {
		t.Errorf("query sem a chave do RG:\n%s", f.queries[0])
	}
}

func TestLastChanges(t *testing.T) {
	f := &argFake{rows: []map[string]any{{"targetId": "/subscriptions/s1/x", "lastChange": "2026-09-20T08:00:00.123Z"}}}
	got, err := LastChanges(context.Background(), fakeARM(f), []string{"s1"}, []string{"/subscriptions/S1/X"})
	if err != nil {
		t.Fatal(err)
	}
	if ts, ok := got["/subscriptions/s1/x"]; !ok || ts.Day() != 20 {
		t.Errorf("LastChanges = %+v", got)
	}
}
