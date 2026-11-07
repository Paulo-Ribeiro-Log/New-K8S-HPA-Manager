package handlers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func csiPV(driver string, attrs map[string]string) corev1.PersistentVolume {
	return corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{
		CSI: &corev1.CSIPersistentVolumeSource{Driver: driver, VolumeAttributes: attrs}}}}
}

func TestSplitSKU(t *testing.T) {
	for in, want := range map[string][2]string{
		"Premium_LRS":     {"Premium", "LRS"},
		"StandardSSD_ZRS": {"StandardSSD", "ZRS"},
		"Standard_RAGZRS": {"Standard", "RAGZRS"},
		"Standard_RA-GRS": {"Standard", "RAGRS"},
		"gp3":             {"gp3", ""},
		"pd-ssd":          {"pd-ssd", ""},
		"UltraSSD_LRS":    {"UltraSSD", "LRS"},
		"Premium_unknown": {"Premium_unknown", ""},
	} {
		if p, r := splitSKU(in); p != want[0] || r != want[1] {
			t.Errorf("%s: (%q,%q), want %v", in, p, r, want)
		}
	}
}

func TestStorageInfoAzureFiles(t *testing.T) {
	pv := csiPV("file.csi.azure.com", map[string]string{"storageAccount": "stfoo", "resourceGroup": "rg-x", "shareName": "s1", "csi.storage.k8s.io/pvc/name": "p"})
	info := storageInfoFor(pv, map[string]string{"skuName": "Standard_GZRS", "protocol": "nfs", "shareAccessTier": "Hot", "secretName": "x"})
	if info == nil || info.SKU != "Standard_GZRS" || info.Redundancy != "GZRS" || info.Performance != "Standard" ||
		info.Tier != "Hot" || info.Protocol != "nfs" || info.Account != "stfoo" || info.ResourceGroup != "rg-x" || info.Share != "s1" {
		t.Fatalf("info = %+v", info)
	}
	if info.RedundancyNote == "" {
		t.Error("deveria explicar a redundância")
	}
	for k := range info.Params {
		if sensitiveParam(k) {
			t.Errorf("param sensível vazou: %s", k)
		}
	}
}

func TestStorageInfoRegionalPD(t *testing.T) {
	info := storageInfoFor(csiPV("pd.csi.storage.gke.io", nil), map[string]string{"type": "pd-ssd", "replication-type": "regional-pd"})
	if info == nil || info.SKU != "pd-ssd" || info.Redundancy != "regional-pd" {
		t.Fatalf("info = %+v", info)
	}
}

func TestStorageInfoNilWhenNothingKnown(t *testing.T) {
	if info := storageInfoFor(csiPV("x.csi", nil), nil); info != nil {
		t.Fatalf("esperava nil, veio %+v", info)
	}
	if info := storageInfoFor(corev1.PersistentVolume{}, nil); info != nil {
		t.Fatalf("PV sem backend: %+v", info)
	}
}
