package gcp

import "testing"

const aggregatedFixture = `{
  "items": {
    "zones/southamerica-east1-a": {"disks": [
      {"id":"1","name":"pvc-aaa","sizeGb":"100","type":"https://www.googleapis.com/compute/v1/projects/p/zones/southamerica-east1-a/diskTypes/pd-balanced",
       "status":"READY","creationTimestamp":"2024-01-01T00:00:00.000-03:00","lastDetachTimestamp":"2024-02-01T00:00:00.000-03:00",
       "zone":"https://www.googleapis.com/compute/v1/projects/p/zones/southamerica-east1-a",
       "selfLink":"https://www.googleapis.com/compute/v1/projects/p/zones/southamerica-east1-a/disks/pvc-aaa",
       "description":"{\"kubernetes.io/created-for/pvc/name\":\"data-0\",\"kubernetes.io/created-for/pvc/namespace\":\"db\",\"kubernetes.io/created-for/pv/name\":\"pvc-aaa\"}"},
      {"id":"2","name":"em-uso","sizeGb":"50","type":".../diskTypes/pd-ssd","status":"READY",
       "users":["https://www.googleapis.com/compute/v1/projects/p/zones/southamerica-east1-a/instances/vm-1"],
       "zone":".../zones/southamerica-east1-a"}
    ]},
    "zones/us-central1-b": {"warning": {"code": "NO_RESULTS_ON_PAGE"}},
    "regions/us-east1": {"disks": [
      {"id":"3","name":"gke-cl-abc-pvc-123","sizeGb":"10","type":".../diskTypes/pd-standard","status":"READY",
       "region":"https://www.googleapis.com/compute/v1/projects/p/regions/us-east1",
       "labels":{"kubernetes_io_created-for_pvc_name":"cache","kubernetes_io_created-for_pvc_namespace":"app"}}
    ]}
  },
  "nextPageToken": "tok2"
}`

func TestParseAggregatedDisks_AndFilterUnattached(t *testing.T) {
	disks, next, err := parseAggregatedDisks([]byte(aggregatedFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if next != "tok2" {
		t.Errorf("nextPageToken = %q", next)
	}
	if len(disks) != 3 {
		t.Fatalf("esperava 3 discos brutos (zona sem disco só tem warning), veio %d", len(disks))
	}

	un := unattachedFromDisks(disks)
	if len(un) != 2 {
		t.Fatalf("disco com users deveria sair: %d desatachados", len(un))
	}

	byName := map[string]int{}
	for i, d := range un {
		byName[d.Name] = i
	}

	aaa := un[byName["pvc-aaa"]]
	if aaa.Provider != "gcp" || aaa.SizeGB != 100 || aaa.DiskType != "pd-balanced" {
		t.Errorf("campos básicos errados: %+v", aaa)
	}
	if aaa.Zone != "southamerica-east1-a" || aaa.Location != "southamerica-east1" {
		t.Errorf("zona/região erradas: zone=%q location=%q", aaa.Zone, aaa.Location)
	}
	if aaa.K8sPVCName != "data-0" || aaa.K8sPVCNamespace != "db" || aaa.K8sPVName != "pvc-aaa" {
		t.Errorf("origem K8s (description JSON) não extraída: %+v", aaa)
	}
	if aaa.UnattachedSince == "" {
		t.Errorf("lastDetachTimestamp deveria virar unattached_since")
	}

	regional := un[byName["gke-cl-abc-pvc-123"]]
	if regional.Zone != "" || regional.Location != "us-east1" {
		t.Errorf("disco regional: zone=%q location=%q", regional.Zone, regional.Location)
	}
	if regional.K8sPVCName != "cache" || regional.K8sPVCNamespace != "app" {
		t.Errorf("origem K8s (labels CSI) não extraída: %+v", regional)
	}
	if regional.K8sClusterHint != "gke-cl-abc-pvc-123" {
		t.Errorf("pista de cluster pelo nome do disco: %q", regional.K8sClusterHint)
	}
}

func TestRegionFromZone(t *testing.T) {
	cases := map[string]string{
		"southamerica-east1-a": "southamerica-east1",
		"us-central1-f":        "us-central1",
		"us-east1":             "us-east1", // já é região: último trecho não é uma letra só
		"":                     "",
	}
	for in, want := range cases {
		if got := regionFromZone(in); got != want {
			t.Errorf("regionFromZone(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestListUnattachedDisks_RequiresProject(t *testing.T) {
	if _, err := ListUnattachedDisks(t.Context(), ""); err == nil {
		t.Error("esperava erro sem projectId")
	}
}

func TestParseAggregatedDisks_HyperdiskProvisionedPerformance(t *testing.T) {
	// A API serializa provisionedIops/provisionedThroughput (int64) como string.
	body := []byte(`{"items":{"zones/southamerica-east1-a":{"disks":[
	  {"id":"9","name":"hd","sizeGb":"100","type":".../diskTypes/hyperdisk-balanced","status":"READY",
	   "provisionedIops":"10000","provisionedThroughput":"500","zone":".../zones/southamerica-east1-a"}]}}}`)
	disks, _, err := parseAggregatedDisks(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	un := unattachedFromDisks(disks)
	if len(un) != 1 || un[0].ProvisionedIOPS != 10000 || un[0].ProvisionedMBps != 500 || un[0].DiskType != "hyperdisk-balanced" {
		t.Errorf("performance provisionada não lida: %+v", un)
	}
}
