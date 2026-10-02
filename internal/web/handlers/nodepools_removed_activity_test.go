package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

const testVMSSRes = "/subscriptions/sub/resourceGroups/MC_rg_aks_brazilsouth/providers/Microsoft.Compute/virtualMachineScaleSets/aks-envviasnspot-39633423-vmss"

func activityEntries(t *testing.T, raw string) []azActivityEntry {
	t.Helper()
	var e []azActivityEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestLoadAKSIdentities(t *testing.T) {
	raw := `{"nodeRG":"MC_rg_aks_brazilsouth","cpPrincipal":null,
	  "uai":{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-akspriv-cp":{"principalId":"F51AB855-D30E-4B48-ADDA-A9B11A18C423","clientId":"c1"}},
	  "kubelet":{"clientId":"k1","objectId":"k2","resourceId":"/subscriptions/sub/resourceGroups/MC_x/providers/Microsoft.ManagedIdentity/userAssignedIdentities/akspriv-agentpool"},
	  "spClientId":"msi"}`
	rg, ids := loadAKSIdentities([]byte(raw))
	if rg != "MC_rg_aks_brazilsouth" {
		t.Errorf("nodeRG = %q", rg)
	}
	if d := ids.ids["f51ab855-d30e-4b48-adda-a9b11a18c423"]; !strings.Contains(d, "control plane") || !strings.Contains(d, "id-akspriv-cp") {
		t.Errorf("principalId do control plane = %q", d)
	}
	if d := ids.ids["k2"]; !strings.Contains(d, "kubelet") {
		t.Errorf("kubelet = %q", d)
	}
	if _, ok := ids.ids["msi"]; ok {
		t.Error(`spClientId "msi" (cluster com managed identity) não é uma identidade`)
	}
}

// Caso real: delete/action no VMSS com caller = GUID da identidade do control plane do AKS, e
// instanceIds no requestbody de outra entrada da mesma operação (correlationId).
func TestParseActivityRemovals_AKSIdentityComInstanceIds(t *testing.T) {
	_, ids := loadAKSIdentities([]byte(`{"nodeRG":"x","uai":{"/subscriptions/s/resourceGroups/r/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-cp":{"principalId":"f51ab855-d30e-4b48-adda-a9b11a18c423","clientId":"c1"}}}`))
	entries := activityEntries(t, `[
	  {"operationName":{"value":"Microsoft.Compute/virtualMachineScaleSets/delete/action","localizedValue":"Delete Virtual Machines in Virtual Machine Scale Set"},
	   "eventTimestamp":"2026-09-25T22:03:22Z","status":{"value":"Succeeded"},"resourceId":"`+testVMSSRes+`",
	   "caller":"f51ab855-d30e-4b48-adda-a9b11a18c423","correlationId":"corr-1","claims":{"appid":"c1"}},
	  {"operationName":{"value":"Microsoft.Compute/virtualMachineScaleSets/delete/action"},
	   "eventTimestamp":"2026-09-25T22:02:00Z","status":{"value":"Started"},"resourceId":"`+testVMSSRes+`",
	   "caller":"f51ab855-d30e-4b48-adda-a9b11a18c423","correlationId":"corr-1",
	   "properties":{"requestbody":"{\"instanceIds\":[\"3\",\"10\"]}"}}
	]`)
	got := parseActivityRemovals(entries, ids, "envviasnspot")
	if len(got) != 2 {
		t.Fatalf("esperava 2 nodes (instanceIds 3 e 10), veio %+v", got)
	}
	names := map[string]*RemovedNodeInfo{}
	for _, n := range got {
		names[n.Name] = n
	}
	n, ok := names["aks-envviasnspot-39633423-vmss000003"]
	if _, ok10 := names["aks-envviasnspot-39633423-vmss00000a"]; !ok || !ok10 {
		t.Fatalf("nomes = %v", names)
	}
	if n.InitiatedByKind != initiatorAKS || !strings.Contains(n.InitiatedBy, "id-cp") {
		t.Errorf("responsável = %q (%s)", n.InitiatedBy, n.InitiatedByKind)
	}
	if !strings.Contains(n.LikelyCause, "autoscaler") || !strings.Contains(n.Details, "Responsável:") {
		t.Errorf("causa = %q, details = %q", n.LikelyCause, n.Details)
	}
}

func TestResolveActivityCaller(t *testing.T) {
	empty := aksIdentities{ids: map[string]string{}, resources: map[string]string{}}
	cases := []struct {
		raw, wantKind, wantSub string
	}{
		{`{"caller":"fulano@empresa.com","claims":{"name":"Fulano"}}`, initiatorUser, "Fulano (fulano@empresa.com)"},
		{`{"caller":"1111","claims":{"appid":"2222"}}`, initiatorSP, "appid 2222"},
		{`{"caller":"x","claims":{"appid":"7319c514-987d-4e9b-ac3d-d38c4f427f4c"}}`, initiatorAKS, "AKS"},
		{`{"caller":"x","claims":{"xms_mirid":"/subscriptions/s/resourcegroups/r/providers/Microsoft.ContainerService/managedClusters/aks1"}}`, initiatorAKS, "cluster AKS"},
		{`{"caller":"x","claims":{"xms_mirid":"/subscriptions/s/resourcegroups/r/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-pipeline"}}`, initiatorMI, "id-pipeline"},
		{`{"caller":""}`, initiatorPlatform, "Plataforma Azure"},
	}
	for _, c := range cases {
		var e azActivityEntry
		if err := json.Unmarshal([]byte(c.raw), &e); err != nil {
			t.Fatal(err)
		}
		who, kind := resolveActivityCaller(e, empty)
		if kind != c.wantKind || !strings.Contains(who, c.wantSub) {
			t.Errorf("%s → %q (%s), want %s contendo %q", c.raw, who, kind, c.wantKind, c.wantSub)
		}
	}
}

// Evento de scale-down do CA (causa específica) + Activity Log (identidade exata): a causa do CA
// prevalece e o responsável vem do Activity Log.
func TestMergeRemovedNode_CausaEspecificaEResponsavelDoActivity(t *testing.T) {
	evt := &RemovedNodeInfo{Name: "n1", Source: "k8s-events", InitiatedBy: "cluster-autoscaler", InitiatedByKind: initiatorAKS,
		LikelyCause: "Scale-down do cluster autoscaler (node vazio ou subutilizado)", causePriority: 2}
	act := &RemovedNodeInfo{Name: "n1", Source: "azure-activity", InitiatedBy: "Identidade do control plane do AKS (id-cp)",
		InitiatedByKind: initiatorAKS, LikelyCause: "Operação do AKS: ...", causePriority: 1, Details: "Responsável: x"}
	mergeRemovedNode(evt, act)
	if !strings.HasPrefix(evt.LikelyCause, "Scale-down") || evt.InitiatedBy != act.InitiatedBy || !strings.Contains(evt.Details, "Responsável") {
		t.Errorf("merge = %+v", evt)
	}
}
