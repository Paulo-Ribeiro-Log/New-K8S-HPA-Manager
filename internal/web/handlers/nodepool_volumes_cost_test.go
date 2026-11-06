package handlers

import (
	"testing"

	"k8s-hpa-manager/internal/finops"

	corev1 "k8s.io/api/core/v1"
)

func costAzurePV(name, handle string, attrs map[string]string) corev1.PersistentVolume {
	pv := vtPV(name, "managed-csi", corev1.VolumeBound, nil)
	pv.Spec.CSI = &corev1.CSIPersistentVolumeSource{Driver: "disk.csi.azure.com", VolumeHandle: handle, VolumeAttributes: attrs}
	pv.Labels = map[string]string{"topology.kubernetes.io/zone": "brazilsouth-1"}
	return pv
}

func TestApplyVolumeCosts(t *testing.T) {
	pvs := []corev1.PersistentVolume{
		costAzurePV("pv-a", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.Compute/disks/pvc-aaa", map[string]string{"skuname": "Premium_LRS"}),
		costAzurePV("pv-b", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.Compute/disks/pvc-bbb", map[string]string{"skuname": "Premium_LRS"}),
		costAzurePV("pv-sc", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.Compute/disks/pvc-ccc", nil), // tipo vem da StorageClass
		costAzurePV("pv-sem-tipo", "/subscriptions/x/resourceGroups/rg/providers/Microsoft.Compute/disks/pvc-ddd", nil),
		vtPV("pv-nfs", "nfs", corev1.VolumeBound, nil),
	}
	pvs[4].Spec.NFS = nil
	items := []VolumeItem{
		{Kind: "pvc", Namespace: "app", PVC: "a", PV: "pv-a", StorageClass: "managed-csi", CapacityBytes: 100 << 30, Workloads: []string{"Deployment/web", "CronJob/backup"}},
		{Kind: "pvc", Namespace: "app", PVC: "b", PV: "pv-b", StorageClass: "managed-csi", CapacityBytes: 10 << 30, Workloads: []string{"Deployment/web"}, Idle: true},
		{Kind: "pvc", Namespace: "app", PVC: "c", PV: "pv-sc", StorageClass: "com-sku", CapacityBytes: 10 << 30, Workloads: []string{"Deployment/web"}},
		{Kind: "pvc", Namespace: "app", PVC: "d", PV: "pv-sem-tipo", StorageClass: "managed-csi", CapacityBytes: 10 << 30},
		{Kind: "pvc", Namespace: "app", PVC: "e", PV: "pv-nfs", StorageClass: "nfs", CapacityBytes: 50 << 30, Workloads: []string{"Deployment/web"}},
		{Kind: "nfs", Namespace: "app", Workloads: []string{"Deployment/web"}},
	}
	in := VolumeCostInputs{
		Provider: "azure", Rate: 5,
		Prices: finops.DiskPriceFuncs{Azure: func(azureType, tier, region string) (float64, string, error) {
			if region != "brazilsouth" {
				t.Errorf("região esperada brazilsouth, veio %q", region)
			}
			return 10, "api", nil // 10 USD por disco (tier)
		}},
		Effective: map[string]float64{"pvc-aaa": 200}, // pv-a tem custo efetivo gravado (pelo nome do disco)
	}
	var sum VolumesSummary
	apps := applyVolumeCosts(items, pvs, map[string]map[string]string{"com-sku": {"skuName": "Premium_LRS"}}, in, &sum)

	if items[0].CostKind != "effective" || items[0].CostBRL != 200 {
		t.Fatalf("a: %+v", items[0])
	}
	if items[1].CostKind != "estimate" || items[1].CostBRL != 50 { // 10 USD × 5
		t.Fatalf("b: %+v", items[1])
	}
	if items[2].CostKind != "estimate" { // tipo veio da StorageClass
		t.Fatalf("c: %+v", items[2])
	}
	if items[3].CostKind != "none" || items[4].CostKind != "none" || items[5].CostKind != "none" {
		t.Fatalf("sem preço: %+v %+v %+v", items[3], items[4], items[5])
	}
	if sum.CostEffective != 1 || sum.CostEstimate != 2 || sum.CostNone != 3 || sum.CostBRL != 300 || sum.CostIdleBRL != 50 {
		t.Fatalf("resumo: %+v", sum)
	}

	by := map[string]AppCost{}
	for _, a := range apps {
		by[a.App] = a
	}
	// web: a/2 (100) + b (50) + c (50) + e + nfs direto; backup: a/2 (100); sem app: d
	if w := by["Deployment/web"]; w.CostBRL != 200 || w.SharedVolumes != 1 || w.Volumes != 5 || w.Unpriced != 1 {
		t.Fatalf("web: %+v", w)
	}
	if b := by["CronJob/backup"]; b.CostBRL != 100 || b.SharedVolumes != 1 {
		t.Fatalf("backup: %+v", b)
	}
	if n := by[noAppLabel]; n.Volumes != 1 || n.Unpriced != 1 {
		t.Fatalf("sem app: %+v", n)
	}
	if apps[0].App != "Deployment/web" && apps[0].CostBRL != 200 {
		t.Fatalf("ordenação por custo: %+v", apps[0])
	}
}
