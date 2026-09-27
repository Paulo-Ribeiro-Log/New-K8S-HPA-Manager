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
		{"subscriptionId": "s1", "resourceGroup": "rg-logistica-shared-hlg", "journey": "Logistica", "envTag": "", "clusterName": "", "source": "rg"},
		{"subscriptionId": "s1", "resourceGroup": "MC_rg-entregas-app-hlg_aks-entregas-hlg_brazilsouth", "journey": "", "clusterName": "aks-entregas-hlg", "source": "node"},
		// Node RG que também tem a tag: não duplica, fica como node.
		{"subscriptionId": "s1", "resourceGroup": "MC_rg-entregas-app-hlg_aks-entregas-hlg_brazilsouth", "journey": "logistica", "clusterName": "", "source": "rg"},
	}}
	clusters := []JourneyCluster{{Name: "aks-entregas-hlg", Journey: "logistica"}}
	rgs, err := ResolveJourneyResourceGroups(context.Background(), fakeARM(f), []string{"s1"}, []string{"Logistica"}, clusters)
	if err != nil {
		t.Fatal(err)
	}
	if len(rgs) != 2 {
		t.Fatalf("esperava 2 RGs, got %+v", rgs)
	}
	for _, rg := range rgs {
		switch rg.ResourceGroup {
		case "rg-logistica-shared-hlg":
			if rg.Source != "tag" || rg.JourneyTag != "Logistica" {
				t.Errorf("RG tagueado: %+v", rg)
			}
		default:
			if rg.Source != "node" || rg.Cluster != "aks-entregas-hlg" || rg.Journey != "logistica" || rg.JourneyTag != "logistica" {
				t.Errorf("node RG: %+v", rg)
			}
		}
	}
	q := f.queries[0]
	for _, part := range []string{"tolower(journey) in ('logistica')", "'aks-entregas-hlg'", "nodeResourceGroup"} {
		if !strings.Contains(q, part) {
			t.Errorf("query não contém %q:\n%s", part, q)
		}
	}
	if _, err := ResolveJourneyResourceGroups(context.Background(), fakeARM(f), []string{"s1"}, nil, clusters); err == nil {
		t.Error("sem jornada deveria falhar (a busca começa pela tag)")
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
		{SubscriptionID: "s1", ResourceGroup: "rg-logistica-shared", Journey: "logistica", JourneyTag: "logistica", Source: "tag"},
	}
	disks, others, err := ListOrphanResources(context.Background(), fakeARM(f), []string{"s1"}, rgs, []string{"logistica"}, "(?i)(^|[-_])(hlg)($|[-_])")
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
	for _, part := range []string{strings.ToLower("s1/" + mc), "rj in ('logistica')", "matches regex @'(?i)(^|[-_])(hlg)($|[-_])'"} {
		if !strings.Contains(f.queries[0], part) {
			t.Errorf("query não contém %q:\n%s", part, f.queries[0])
		}
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

func TestEffectiveJourney(t *testing.T) {
	node := models.ScopedResourceGroup{Source: "node", Journey: "logistica"}
	app := models.ScopedResourceGroup{Source: "cluster", Journey: "logistica"} // RG de app sem tag: pode ser compartilhado
	tagged := models.ScopedResourceGroup{Source: "tag", Journey: "backoffice", JourneyTag: "backoffice"}
	cases := []struct {
		tags                    map[string]string
		rg                      models.ScopedResourceGroup
		wantJourney, wantSource string
	}{
		{map[string]string{"Jornada": "vendas"}, tagged, "vendas", "resource_tag"}, // tag do recurso vence a do RG
		{nil, tagged, "backoffice", "rg_tag"},
		{nil, node, "logistica", "node_rg"},
		{nil, app, "", "none"},
	}
	for i, c := range cases {
		if j, src := effectiveJourney(c.tags, c.rg); j != c.wantJourney || src != c.wantSource {
			t.Errorf("caso %d: (%q, %q), want (%q, %q)", i, j, src, c.wantJourney, c.wantSource)
		}
	}
}
