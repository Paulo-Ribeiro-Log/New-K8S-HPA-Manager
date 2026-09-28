package finops

import (
	"sort"
	"strings"
	"time"

	"k8s-hpa-manager/internal/models"
)

// Recursos órfãos (não-disco) de uma jornada — idade e custo estimado. A descoberta é do
// cloudprovider/azure (Resource Graph); aqui só a parte de FinOps.

const (
	// OrphanMinAgeDays: órfão "confirmado" só depois de 7 dias sem alteração (pedido do usuário);
	// abaixo disso pode ser uma troca em andamento (VM recriada, NIC trocada de VM...).
	OrphanMinAgeDays = 7
	// resourceChangesWindowDays: retenção da tabela resourcechanges do Resource Graph.
	resourceChangesWindowDays = 14
)

// AnnotateOrphanAges preenche LastChange/SinceBasis/AgeDays/Recent. lastChanges é indexado por
// resource ID em minúsculas (azure.LastChanges); recurso sem entrada = nenhuma alteração na janela.
func AnnotateOrphanAges(items []models.OrphanResource, lastChanges map[string]time.Time, now time.Time) {
	for i := range items {
		o := &items[i]
		if t, ok := lastChanges[strings.ToLower(o.ID)]; ok {
			o.LastChange = t.UTC().Format(time.RFC3339)
			o.SinceBasis = "last_change"
			o.AgeDays = max(int(now.Sub(t).Hours()/24), 0)
		} else {
			o.SinceBasis = "no_change_14d"
			o.AgeDays = resourceChangesWindowDays
		}
		o.Recent = o.AgeDays < OrphanMinAgeDays
	}
}

// orphanListPriceUSD é o preço de lista mensal (USD, ~730 h, sem tráfego) dos tipos que cobram só
// por existir. Valores de referência da tabela pública do Azure — ordem de grandeza, não fatura.
func orphanListPriceUSD(o models.OrphanResource) (usd float64, note string) {
	sku := strings.ToLower(o.SKU)
	switch o.Type {
	case "microsoft.network/publicipaddresses":
		if strings.Contains(sku, "basic") {
			return 2.63, "IP público Basic (preço de lista)"
		}
		return 3.65, "IP público Standard (preço de lista)"
	case "microsoft.network/loadbalancers":
		if strings.Contains(sku, "basic") {
			return 0, "Load Balancer Basic não é cobrado"
		}
		return 18.25, "Load Balancer Standard, 5 primeiras regras, sem tráfego (preço de lista)"
	case "microsoft.network/natgateways":
		return 32.85, "NAT Gateway por hora, sem tráfego (preço de lista)"
	case "microsoft.network/applicationgateways":
		switch {
		case strings.Contains(sku, "waf_v2"):
			return 323.39, "Application Gateway WAF_v2, só a parte fixa (preço de lista)"
		case strings.Contains(sku, "standard_v2"):
			return 179.58, "Application Gateway Standard_v2, só a parte fixa (preço de lista)"
		}
		return 0, "Application Gateway v1: sem estimativa"
	case "microsoft.network/privateendpoints":
		return 7.30, "Private Endpoint por hora, sem tráfego (preço de lista)"
	case "microsoft.network/privatednszones":
		return 0.50, "Private DNS zone (preço de lista)"
	case "microsoft.compute/virtualmachines":
		if strings.Contains(o.Reason, "parada") {
			return 0, "VM parada sem desalocar: o compute (" + o.SKU + ") continua cobrado — desaloque ou exclua"
		}
		return 0, "Compute não é cobrado; os discos da VM continuam (não aparecem na lista de discos por estarem atachados)"
	case "microsoft.web/serverfarms":
		return 0, "Custo depende do SKU do plano (" + o.SKU + ")"
	}
	return 0, "Sem custo direto"
}

// PriceOrphanResources preenche o custo estimado e o comando de exclusão sugerido.
func PriceOrphanResources(items []models.OrphanResource, rate float64, deleteCommand func(models.OrphanResource) string) {
	if rate <= 0 {
		rate = DefaultExchangeRate
	}
	for i := range items {
		usd, note := orphanListPriceUSD(items[i])
		items[i].MonthlyCostUSD = round2(usd)
		items[i].MonthlyCostBRL = round2(usd * rate)
		items[i].PriceNote = note
		if deleteCommand != nil {
			items[i].DeleteCommand = deleteCommand(items[i])
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MonthlyCostBRL != items[j].MonthlyCostBRL {
			return items[i].MonthlyCostBRL > items[j].MonthlyCostBRL
		}
		return items[i].AgeDays > items[j].AgeDays
	})
}

// OrphanSummary resume os órfãos não-disco (os discos têm o resumo próprio do relatório de discos).
type OrphanSummary struct {
	TotalCount   int            `json:"total_count"`
	AgedCount    int            `json:"aged_count"` // sem alteração há 7+ dias
	RecentCount  int            `json:"recent_count"`
	TotalCostBRL float64        `json:"total_cost_brl"`
	AgedCostBRL  float64        `json:"aged_cost_brl"`
	ByType       map[string]int `json:"by_type"`
}

func SummarizeOrphans(items []models.OrphanResource) OrphanSummary {
	s := OrphanSummary{ByType: map[string]int{}}
	for _, o := range items {
		s.TotalCount++
		s.ByType[o.Type]++
		s.TotalCostBRL = round2(s.TotalCostBRL + o.MonthlyCostBRL)
		if o.Recent {
			s.RecentCount++
		} else {
			s.AgedCount++
			s.AgedCostBRL = round2(s.AgedCostBRL + o.MonthlyCostBRL)
		}
	}
	return s
}

// IgnoredByJourney conta os recursos (discos + demais) deixados de fora pelo filtro de jornada.
type IgnoredByJourney struct {
	OtherJourney int `json:"other_journey"` // jornada (tag) diferente da selecionada
	NoJourney    int `json:"no_journey"`    // sem tag no recurso nem no RG e fora de node RG
}

// FilterByJourney mantém só os recursos cuja jornada efetiva (tag do recurso → tag do RG → node RG
// do cluster; ver azure.effectiveJourney) está em journeys (comparação sem diferenciar caixa).
// journeys vazio = qualquer jornada, mas recurso sem jornada continua de fora.
func FilterByJourney(disks []models.UnattachedDisk, others []models.OrphanResource, journeys []string) ([]models.UnattachedDisk, []models.OrphanResource, IgnoredByJourney) {
	want := map[string]bool{}
	for _, j := range journeys {
		want[strings.ToLower(strings.TrimSpace(j))] = true
	}
	var ignored IgnoredByJourney
	keep := func(journey string) bool {
		switch {
		case strings.TrimSpace(journey) == "":
			ignored.NoJourney++
			return false
		case len(want) > 0 && !want[strings.ToLower(strings.TrimSpace(journey))]:
			ignored.OtherJourney++
			return false
		}
		return true
	}
	var keptDisks []models.UnattachedDisk
	for _, d := range disks {
		if keep(d.Journey) {
			keptDisks = append(keptDisks, d)
		}
	}
	var keptOthers []models.OrphanResource
	for _, o := range others {
		if keep(o.Journey) {
			keptOthers = append(keptOthers, o)
		}
	}
	return keptDisks, keptOthers, ignored
}
