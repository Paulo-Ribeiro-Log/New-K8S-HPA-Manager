package finops

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"k8s-hpa-manager/internal/cloudprovider/azure"
	"k8s-hpa-manager/internal/storage"
)

// Índice de SKUs que já rodam sob reserva / Savings Plan na frota, usado nas trocas de SKU.
//
// A regra padrão do SuggestVMTier oferece só a geração mais nova (D2s_v5). Se a frota já tem reserva
// de D2s_v4, a v4 sai mais barata na prática — então o índice (1) etiqueta cada alternativa com a
// cobertura conhecida e (2) inclui o "gêmeo" de geração anterior quando ele já roda coberto.
//
// LIMITE: o índice vem do uso OBSERVADO no Cost Management. Diz "este SKU já roda sob reserva", não
// "há capacidade ociosa": reserva ociosa não aparece sem o papel Reservations Reader.

const (
	// minIndexedMonthlyCost: custo amortizado mensal (R$) abaixo do qual a cobertura é ruído.
	minIndexedMonthlyCost = 50.0
	// maxAlternativesWithTwins: teto de alternativas depois de incluir os gêmeos (o SuggestVMTier
	// já limita a 3).
	maxAlternativesWithTwins = 4

	HintKindReservation = "reservation"
	HintKindSavingsPlan = "savings_plan"
	HintScopeSKU        = "sku"    // este SKU exato roda coberto
	HintScopeSeries     = "series" // outro tamanho da mesma série roda coberto (vale com flexibilidade de tamanho)
)

// SKUCoverageHint diz que um SKU (ou a série dele) já roda sob reserva/Savings Plan na frota.
type SKUCoverageHint struct {
	Kind  string `json:"kind"`  // reservation | savings_plan
	Scope string `json:"scope"` // sku | series
	Note  string `json:"note"`
}

// SKUCoverageIndex agrega, por SKU e por série, o custo amortizado MENSAL sob cada modelo de preço.
type SKUCoverageIndex struct {
	bySKU    map[string]map[string]float64 // SKU em minúsculas → modelo → R$/mês
	bySeries map[string]map[string]float64 // SeriesKey → modelo → R$/mês
}

// Empty diz se não há nenhuma cobertura conhecida (índice nunca consultado ou frota sem reservas).
func (ix SKUCoverageIndex) Empty() bool { return len(ix.bySKU) == 0 }

// BuildSKUCoverageIndex monta o índice a partir do que está gravado (todas as subscriptions).
func BuildSKUCoverageIndex(rows []storage.SKUPricingCoverage) SKUCoverageIndex {
	ix := SKUCoverageIndex{bySKU: map[string]map[string]float64{}, bySeries: map[string]map[string]float64{}}
	for _, r := range rows {
		monthly := r.Cost
		if r.WindowDays > 0 {
			monthly = r.Cost * (float64(HoursPerMonth) / 24) / float64(r.WindowDays)
		}
		if monthly < minIndexedMonthlyCost {
			continue
		}
		model := r.Model
		if model != azure.ModelReservation && model != azure.ModelSavingsPlan {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(r.SKU))
		if ix.bySKU[key] == nil {
			ix.bySKU[key] = map[string]float64{}
		}
		ix.bySKU[key][model] += monthly
		if series := SeriesKey(r.SKU); series != "" {
			if ix.bySeries[series] == nil {
				ix.bySeries[series] = map[string]float64{}
			}
			ix.bySeries[series][model] += monthly
		}
	}
	return ix
}

func kindOf(model string) string {
	if model == azure.ModelSavingsPlan {
		return HintKindSavingsPlan
	}
	return HintKindReservation
}

// Hint devolve a melhor evidência de cobertura para o SKU: reserva do próprio SKU, Savings Plan do
// próprio SKU, reserva de outro tamanho da mesma série. Savings Plan só vale por SKU exato: ele cobre
// várias famílias, então "outro tamanho da série sob plano" não diz nada sobre este SKU. nil = nada.
func (ix SKUCoverageIndex) Hint(sku string) *SKUCoverageHint {
	if ix.Empty() {
		return nil
	}
	if m := ix.bySKU[strings.ToLower(strings.TrimSpace(sku))]; m != nil {
		if v := m[azure.ModelReservation]; v > 0 {
			return &SKUCoverageHint{Kind: HintKindReservation, Scope: HintScopeSKU,
				Note: fmt.Sprintf("%s já roda sob reserva na frota (~%s/mês amortizado em uso). Não dá para saber se há capacidade ociosa — só que a reserva existe.", sku, brlInt(v))}
		}
		if v := m[azure.ModelSavingsPlan]; v > 0 {
			return &SKUCoverageHint{Kind: HintKindSavingsPlan, Scope: HintScopeSKU,
				Note: fmt.Sprintf("%s já roda sob Savings Plan na frota (~%s/mês amortizado em uso). O plano cobre várias famílias; não dá para saber se sobra compromisso.", sku, brlInt(v))}
		}
	}
	if series := SeriesKey(sku); series != "" {
		if v := ix.bySeries[series][azure.ModelReservation]; v > 0 {
			return &SKUCoverageHint{Kind: HintKindReservation, Scope: HintScopeSeries,
				Note: fmt.Sprintf("Há reserva ativa de outro tamanho da mesma série de %s (~%s/mês amortizado em uso): com flexibilidade de tamanho ela pode cobrir. Não dá para saber se há capacidade ociosa.", sku, brlInt(v))}
		}
	}
	return nil
}

// hintRank ordena as alternativas: cobertura exata por reserva primeiro, depois plano, depois
// reserva da série, depois sem cobertura.
func hintRank(h *SKUCoverageHint) int {
	switch {
	case h == nil:
		return 3
	case h.Scope == HintScopeSKU && h.Kind == HintKindReservation:
		return 0
	case h.Scope == HintScopeSKU && h.Kind == HintKindSavingsPlan:
		return 1
	default:
		return 2
	}
}

func verdictRank(v string) int {
	switch v {
	case "recommended":
		return 0
	case "consider":
		return 1
	case "cheaper":
		return 2
	}
	return 3
}

// azureSKURe decompõe um SKU Azure: standard_<letras><vCPUs><sufixo>_v<geração>.
var azureSKURe = regexp.MustCompile(`^standard_([a-z]+)(\d+)([a-z]*)_v(\d+)$`)

// azureGenerationTwins devolve os SKUs da tabela azureVMTierSpecs com a MESMA configuração (letras,
// vCPUs, sufixo e memória) em outra geração — D4s_v5 → D4s_v4, D4s_v3. Ordem estável.
func azureGenerationTwins(sku string) []string {
	m := azureSKURe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(sku)))
	if m == nil {
		return nil
	}
	self, ok := lookupAzureSpec(sku)
	if !ok {
		return nil
	}
	var out []string
	for name, spec := range azureVMTierSpecs {
		tm := azureSKURe.FindStringSubmatch(strings.ToLower(name))
		if tm == nil || tm[1] != m[1] || tm[2] != m[2] || tm[3] != m[3] || tm[4] == m[4] {
			continue
		}
		if spec.MemoryGB != self.MemoryGB {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func lookupAzureSpec(sku string) (VMSpec, bool) {
	for name, spec := range azureVMTierSpecs {
		if strings.EqualFold(name, sku) {
			return spec, true
		}
	}
	return VMSpec{}, false
}

// AugmentWithCoveredTwins inclui, ao lado de cada alternativa, o gêmeo de geração anterior quando ele
// já roda sob reserva/Savings Plan na frota, e reordena (veredito, depois cobertura). Só AKS; sem
// índice devolve as alternativas como vieram. Preço/economia continuam a preço de TABELA — a
// cobertura só entra como etiqueta e como critério de oferta.
func AugmentWithCoveredTwins(provider, currentSKU string, alts []VMAlternative, cpuUtilPct, memUtilPct float64,
	pricer CloudPricer, exchangeRate float64, nodeCount int, ix SKUCoverageIndex) []VMAlternative {

	if provider != "aks" || ix.Empty() || pricer == nil || len(alts) == 0 {
		return alts
	}
	currentCPU, currentMem := pricer.GetVMSpecs(currentSKU)
	currentPrice, _, err := pricer.GetPrice(currentSKU)
	if currentCPU == 0 || currentMem == 0 || err != nil || currentPrice <= 0 {
		return alts
	}

	out := append([]VMAlternative(nil), alts...)
	seen := map[string]bool{strings.ToLower(currentSKU): true}
	for _, a := range alts {
		seen[strings.ToLower(a.VMSize)] = true
	}
	for _, a := range alts {
		for _, twin := range azureGenerationTwins(a.VMSize) {
			if len(out) >= maxAlternativesWithTwins {
				break
			}
			key := strings.ToLower(twin)
			if seen[key] {
				continue
			}
			hint := ix.Hint(twin)
			if hint == nil || hint.Scope != HintScopeSKU { // só quando o SKU EXATO roda coberto
				continue
			}
			alt, ok := buildVMAlternative(pricer, twin, a.Verdict, currentCPU, currentMem, currentPrice, cpuUtilPct, memUtilPct, exchangeRate, nodeCount)
			if !ok {
				continue
			}
			label := "reserva"
			if hint.Kind == HintKindSavingsPlan {
				label = "Savings Plan"
			}
			alt.Reason = fmt.Sprintf("%s Mesma configuração da geração anterior (%s), que já roda sob %s na frota.", a.Reason, twin, label)
			seen[key] = true
			out = append(out, alt)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if vi, vj := verdictRank(out[i].Verdict), verdictRank(out[j].Verdict); vi != vj {
			return vi < vj
		}
		if hi, hj := hintRank(ix.Hint(out[i].VMSize)), hintRank(ix.Hint(out[j].VMSize)); hi != hj {
			return hi < hj
		}
		// Mesma evidência de cobertura: a geração mais nova primeiro (D4s_v4 antes de D4s_v3).
		return azureGeneration(out[i].VMSize) > azureGeneration(out[j].VMSize)
	})
	return out
}

// azureGeneration devolve o número da geração do SKU (v4 → 4); 0 se não reconhecer.
func azureGeneration(sku string) int {
	m := azureSKURe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(sku)))
	if m == nil {
		return 0
	}
	n := 0
	for _, c := range m[4] {
		n = n*10 + int(c-'0')
	}
	return n
}
