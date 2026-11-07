package handlers

import (
	"fmt"

	"k8s-hpa-manager/internal/finops"
)

// VolumeCostInputs implementa VolumeCostSource para a visão Volumes da aba Node Pools: entrega os
// preços de disco do cloud do cluster, a cotação e o custo efetivo gravado pelo Cost Management.
func (h *FinOpsHandler) VolumeCostInputs(cluster string) (VolumeCostInputs, error) {
	scope, err := h.resolveDiskScope(cluster)
	if err != nil {
		return VolumeCostInputs{}, err
	}
	rate, _ := h.exchange.Get()
	if rate <= 0 {
		return VolumeCostInputs{}, fmt.Errorf("cotação USD→BRL indisponível")
	}
	in := VolumeCostInputs{Provider: scope.provider, Prices: h.diskPriceFuncs(cluster, scope), Rate: rate}
	if h.rightsizingStore != nil {
		if rows, err := h.rightsizingStore.ListDiskCost(cluster); err == nil {
			in.Effective = finops.EffectiveDiskMonthlyBRL(rows)
		}
	}
	return in, nil
}
