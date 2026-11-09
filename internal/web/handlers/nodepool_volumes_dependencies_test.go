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
		{Kind: "pvc", Namespace: "a", PVC: "disco", PV: "pvc-8c091ac5", Source: "Azure Disk", Workloads: []string{"StatefulSet/kafka"},
			Storage: &StorageInfo{Driver: "disk.csi.azure.com", ResourceID: "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/disks/kubernetes-dynamic-pvc-8c091ac5"}},
	}
	got := volumeDependencyRecords("c1", items)
	if len(got) != 6 {
		t.Fatalf("esperava 6 registros (2 workloads + órfão blob + nfs + pv órfão + disco), veio %d: %+v", len(got), got)
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
	if d := got[5]; d.ServiceName != "kubernetes-dynamic-pvc-8c091ac5" || d.TopicName != "Azure Disk" ||
		d.SourceKey != "PV pvc-8c091ac5 · StatefulSet/kafka" || d.Deployment != "kafka" {
		t.Errorf("disco: %+v", d)
	}
	for _, r := range got {
		if r.SourceType != "pvc" || r.ServiceType != "storage" || r.Cluster != "c1" {
			t.Errorf("campos fixos: %+v", r)
		}
	}
}
