package handlers

import "testing"

func TestVolumeDependencyRecords(t *testing.T) {
	items := []VolumeItem{
		{Kind: "pvc", Namespace: "a", PVC: "docs", Source: "Azure Files", Workloads: []string{"Deployment/api", "CronJob/job"},
			Storage: &StorageInfo{Driver: "file.csi.azure.com", Account: "StAcct", Share: "docs"}},
		{Kind: "pvc", Namespace: "a", PVC: "imgs", Source: "CSI: blob.csi.azure.com",
			Storage: &StorageInfo{Driver: "blob.csi.azure.com", Account: "stacct", Share: "imgs"}},
		{Kind: "nfs", Namespace: "b", Source: "NFS (direto no pod)", SourceDetail: "10.0.0.5:/export/x", Workloads: []string{"StatefulSet/db"}},
		{Kind: "pv", PV: "pv-old", Source: "NFS", SourceDetail: "nas.local:/vol"},
		{Kind: "pvc", Namespace: "a", PVC: "disco", Source: "Azure Disk", Storage: &StorageInfo{Driver: "disk.csi.azure.com"}},
	}
	got := volumeDependencyRecords("c1", items)
	if len(got) != 5 {
		t.Fatalf("esperava 5 registros (2 workloads + órfão blob + nfs + pv órfão), veio %d: %+v", len(got), got)
	}
	if got[0].ServiceName != "stacct.file.core.windows.net" || got[0].TopicName != "docs" || got[0].Deployment != "api" || got[1].Deployment != "job" {
		t.Errorf("files: %+v %+v", got[0], got[1])
	}
	if got[2].ServiceName != "stacct.blob.core.windows.net" || got[2].Deployment != "" {
		t.Errorf("blob órfão: %+v", got[2])
	}
	if got[3].ServiceName != "10.0.0.5" || got[3].TopicName != "/export/x" || got[3].Deployment != "db" {
		t.Errorf("nfs: %+v", got[3])
	}
	if got[4].Namespace != "-" || got[4].ServiceName != "nas.local" || got[4].SourceName != "pv-old" {
		t.Errorf("pv órfão: %+v", got[4])
	}
	for _, r := range got {
		if r.SourceType != "pvc" || r.ServiceType != "storage" || r.Cluster != "c1" {
			t.Errorf("campos fixos: %+v", r)
		}
	}
}
