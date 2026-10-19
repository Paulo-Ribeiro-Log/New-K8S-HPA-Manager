package finops

import (
	"testing"

	"k8s-hpa-manager/internal/storage"
)

func TestAttachDiskCost(t *testing.T) {
	r := &FinOpsReport{
		NodePools: []FinOpsPool{{Name: "calculofrete"}, {Name: "spot"}},
		PVCs:      []PVCCostItem{{Name: "data", BoundPV: "pvc-ABC"}, {Name: "x", BoundPV: "pvc-zzz"}},
	}
	AttachDiskCost(r, []storage.DiskCost{
		{Kind: "os", Name: "calculofrete", Cost: 3000, Currency: "BRL", WindowDays: 30},
		{Kind: "disk", Name: "pvc-abc", Cost: 60, Currency: "BRL", WindowDays: 30},
		{Kind: "disk", Name: "pvc-zzz", Cost: 99, Currency: "USD", WindowDays: 30},
	})
	if got := r.NodePools[0].OSDiskEffectiveBRL; got < 3041 || got > 3042 { // 3000 × 30,42/30
		t.Errorf("OS efetivo = %v", got)
	}
	if r.NodePools[1].OSDiskEffectiveBRL != 0 || r.PVCs[1].EffectiveCostBRL != 0 {
		t.Errorf("sem dado (ou moeda ≠ BRL) fica 0: %+v %+v", r.NodePools[1], r.PVCs[1])
	}
	if got := r.PVCs[0].EffectiveCostBRL; got < 60.8 || got > 60.9 {
		t.Errorf("PVC efetivo = %v", got)
	}
}
