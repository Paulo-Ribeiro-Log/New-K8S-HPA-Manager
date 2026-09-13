package finops

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// VMSpec descreve as specs de uma VM/instância, independente de cloud provider.
type VMSpec struct {
	Size     string // nome do SKU/instance type
	VCPUs    int
	MemoryGB int
	Family   string // nome de família nomeada (ex: "Dsv5-Series" no Azure, "e2" no GCP, "m5" na AWS)
}

// ── Azure ────────────────────────────────────────────────────────────────────
//
// azureVMTierSpecs é a tabela de SKUs Azure usada especificamente pra descobrir NOMES reais de
// SKU por família/geração/vCPU — não dá pra "adivinhar" um nome de SKU só por family+vCPUs (o
// sufixo de geração "v3"/"v4"/"v5" nem sempre existe pra todo vCPU count, e cada família tem sua
// própria progressão). Portada de internal/monitoring/predictions/azure_vm_specs.go (tabela mais
// completa que existe no repo, ~85 SKUs com família nomeada corretamente) — a tabela pequena em
// azure_pricing.go:vmSpecs (~27 SKUs) continua existindo só como fallback de specs pra SKUs fora
// desta lista (ex: já configurados num cluster real mas não cobertos aqui).
var azureVMTierSpecs = map[string]VMSpec{
	"Standard_D2s_v3": {"Standard_D2s_v3", 2, 8, "Dsv3-Series"}, "Standard_D4s_v3": {"Standard_D4s_v3", 4, 16, "Dsv3-Series"},
	"Standard_D8s_v3": {"Standard_D8s_v3", 8, 32, "Dsv3-Series"}, "Standard_D16s_v3": {"Standard_D16s_v3", 16, 64, "Dsv3-Series"},
	"Standard_D32s_v3": {"Standard_D32s_v3", 32, 128, "Dsv3-Series"}, "Standard_D48s_v3": {"Standard_D48s_v3", 48, 192, "Dsv3-Series"},
	"Standard_D64s_v3": {"Standard_D64s_v3", 64, 256, "Dsv3-Series"},
	"Standard_D2s_v4":  {"Standard_D2s_v4", 2, 8, "Dsv4-Series"}, "Standard_D4s_v4": {"Standard_D4s_v4", 4, 16, "Dsv4-Series"},
	"Standard_D8s_v4": {"Standard_D8s_v4", 8, 32, "Dsv4-Series"}, "Standard_D16s_v4": {"Standard_D16s_v4", 16, 64, "Dsv4-Series"},
	"Standard_D32s_v4": {"Standard_D32s_v4", 32, 128, "Dsv4-Series"}, "Standard_D48s_v4": {"Standard_D48s_v4", 48, 192, "Dsv4-Series"},
	"Standard_D64s_v4": {"Standard_D64s_v4", 64, 256, "Dsv4-Series"},
	"Standard_D2s_v5":  {"Standard_D2s_v5", 2, 8, "Dsv5-Series"}, "Standard_D4s_v5": {"Standard_D4s_v5", 4, 16, "Dsv5-Series"},
	"Standard_D8s_v5": {"Standard_D8s_v5", 8, 32, "Dsv5-Series"}, "Standard_D16s_v5": {"Standard_D16s_v5", 16, 64, "Dsv5-Series"},
	"Standard_D32s_v5": {"Standard_D32s_v5", 32, 128, "Dsv5-Series"}, "Standard_D48s_v5": {"Standard_D48s_v5", 48, 192, "Dsv5-Series"},
	"Standard_D64s_v5": {"Standard_D64s_v5", 64, 256, "Dsv5-Series"}, "Standard_D96s_v5": {"Standard_D96s_v5", 96, 384, "Dsv5-Series"},
	"Standard_E2s_v3": {"Standard_E2s_v3", 2, 16, "Esv3-Series"}, "Standard_E4s_v3": {"Standard_E4s_v3", 4, 32, "Esv3-Series"},
	"Standard_E8s_v3": {"Standard_E8s_v3", 8, 64, "Esv3-Series"}, "Standard_E16s_v3": {"Standard_E16s_v3", 16, 128, "Esv3-Series"},
	"Standard_E20s_v3": {"Standard_E20s_v3", 20, 160, "Esv3-Series"}, "Standard_E32s_v3": {"Standard_E32s_v3", 32, 256, "Esv3-Series"},
	"Standard_E48s_v3": {"Standard_E48s_v3", 48, 384, "Esv3-Series"}, "Standard_E64s_v3": {"Standard_E64s_v3", 64, 432, "Esv3-Series"},
	"Standard_E2s_v4": {"Standard_E2s_v4", 2, 16, "Esv4-Series"}, "Standard_E4s_v4": {"Standard_E4s_v4", 4, 32, "Esv4-Series"},
	"Standard_E8s_v4": {"Standard_E8s_v4", 8, 64, "Esv4-Series"}, "Standard_E16s_v4": {"Standard_E16s_v4", 16, 128, "Esv4-Series"},
	"Standard_E20s_v4": {"Standard_E20s_v4", 20, 160, "Esv4-Series"}, "Standard_E32s_v4": {"Standard_E32s_v4", 32, 256, "Esv4-Series"},
	"Standard_E48s_v4": {"Standard_E48s_v4", 48, 384, "Esv4-Series"}, "Standard_E64s_v4": {"Standard_E64s_v4", 64, 504, "Esv4-Series"},
	"Standard_E2s_v5": {"Standard_E2s_v5", 2, 16, "Esv5-Series"}, "Standard_E4s_v5": {"Standard_E4s_v5", 4, 32, "Esv5-Series"},
	"Standard_E8s_v5": {"Standard_E8s_v5", 8, 64, "Esv5-Series"}, "Standard_E16s_v5": {"Standard_E16s_v5", 16, 128, "Esv5-Series"},
	"Standard_E20s_v5": {"Standard_E20s_v5", 20, 160, "Esv5-Series"}, "Standard_E32s_v5": {"Standard_E32s_v5", 32, 256, "Esv5-Series"},
	"Standard_E48s_v5": {"Standard_E48s_v5", 48, 384, "Esv5-Series"}, "Standard_E64s_v5": {"Standard_E64s_v5", 64, 512, "Esv5-Series"},
	"Standard_E96s_v5": {"Standard_E96s_v5", 96, 672, "Esv5-Series"},
	"Standard_F2s_v2":  {"Standard_F2s_v2", 2, 4, "Fsv2-Series"}, "Standard_F4s_v2": {"Standard_F4s_v2", 4, 8, "Fsv2-Series"},
	"Standard_F8s_v2": {"Standard_F8s_v2", 8, 16, "Fsv2-Series"}, "Standard_F16s_v2": {"Standard_F16s_v2", 16, 32, "Fsv2-Series"},
	"Standard_F32s_v2": {"Standard_F32s_v2", 32, 64, "Fsv2-Series"}, "Standard_F48s_v2": {"Standard_F48s_v2", 48, 96, "Fsv2-Series"},
	"Standard_F64s_v2": {"Standard_F64s_v2", 64, 128, "Fsv2-Series"}, "Standard_F72s_v2": {"Standard_F72s_v2", 72, 144, "Fsv2-Series"},
	"Standard_B2s": {"Standard_B2s", 2, 4, "B-Series"}, "Standard_B2ms": {"Standard_B2ms", 2, 8, "B-Series"},
	"Standard_B4ms": {"Standard_B4ms", 4, 16, "B-Series"}, "Standard_B8ms": {"Standard_B8ms", 8, 32, "B-Series"},
}

// azureFamilyLetter extrai a letra-base da família (D/E/F/B) do nome do SKU — mesma lógica já
// usada em vm_alternatives.go (extractVMFamily), preservada aqui pra decidir o GRUPO de família
// (troca D↔E↔F), não a progressão de tamanho dentro da família (isso é azureFamilyTiers).
func azureFamilyLetter(vmSize string) string {
	name := strings.TrimPrefix(vmSize, "Standard_")
	if len(name) == 0 {
		return ""
	}
	return strings.ToUpper(string(name[0]))
}

// azureFamilyTiers retorna os specs de uma letra de família (D/E/F/B), ordenados por vCPUs asc,
// mantendo só a geração mais recente disponível por vCPU count (evita D4s_v3/v4/v5 competindo
// como 3 candidatos "iguais" — a versão mais nova é sempre a melhor escolha em preço/perf).
func azureFamilyTiers(letter string) []VMSpec {
	bestByVCPU := make(map[int]VMSpec)
	genRank := map[string]int{"v5": 3, "v4": 2, "v3": 1, "v2": 0}
	for _, spec := range azureVMTierSpecs {
		if azureFamilyLetter(spec.Size) != letter {
			continue
		}
		gen := "v2"
		for g := range genRank {
			if strings.HasSuffix(spec.Size, "_"+g) {
				gen = g
				break
			}
		}
		if cur, ok := bestByVCPU[spec.VCPUs]; !ok || genRank[gen] > genRank[currentGen(cur.Size)] {
			bestByVCPU[spec.VCPUs] = spec
		}
	}
	result := make([]VMSpec, 0, len(bestByVCPU))
	for _, spec := range bestByVCPU {
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].VCPUs < result[j].VCPUs })
	return result
}

func currentGen(size string) string {
	for _, g := range []string{"v5", "v4", "v3", "v2"} {
		if strings.HasSuffix(size, "_"+g) {
			return g
		}
	}
	return "v2"
}

// ── GCP ──────────────────────────────────────────────────────────────────────
//
// gcpTierVCPULadder é a progressão de vCPUs comum às famílias listadas em gcpMachineFamilies
// (gcp_pricing.go). Diferente do Azure, GCE aceita machine types com qualquer combinação válida
// de <família>-<tier>-<vCPUs> nesses passos — a Cloud Billing Catalog API precifica por
// vCPU×GB independente do SKU ser um "tamanho oficialmente catalogado" ou não (confirmado pela
// fórmula já usada em GetPrice: core_usd_hora×vCPUs + ram_usd_gb_hora×RAM), então esta lista é
// segura mesmo sendo uma progressão sintética, não uma tabela de SKUs reais catalogados.
var gcpTierVCPULadder = []int{2, 4, 8, 16, 32, 48, 64}

// ── AWS ──────────────────────────────────────────────────────────────────────
//
// awsFamilyGroups agrupa famílias comuns na frota desta empresa por perfil (propósito
// geral/compute/memória) e a progressão de tamanho de cada uma — curada (não é o catálogo EC2
// inteiro, que tem centenas de famílias incluindo GPU/bare-metal, fora de escopo, mesmo critério
// de exclusão já usado em internal/monitoring/nodepoolpredictions/cost_analyzer.go). Specs/preço
// de cada instance type são resolvidos sob demanda via AWSPricer (cache + AWS Pricing API), não
// hardcoded aqui — só os NOMES são curados.
var awsFamilyGroups = map[string][]string{ // grupo → famílias, ordem = preferência de troca
	"general": {"t3", "m6i", "m5"},
	"compute": {"c6i", "c5"},
	"memory":  {"r6i", "r5"},
}

var awsSizesBySize = []string{"nano", "micro", "small", "medium", "large", "xlarge", "2xlarge", "4xlarge", "8xlarge"}

func awsFamilyGroupOf(family string) string {
	for group, families := range awsFamilyGroups {
		for _, f := range families {
			if f == family {
				return group
			}
		}
	}
	return ""
}

func awsParseInstanceType(instanceType string) (family, size string, ok bool) {
	parts := strings.SplitN(instanceType, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func awsSizeIndex(size string) int {
	for i, s := range awsSizesBySize {
		if s == size {
			return i
		}
	}
	return -1
}

// ── Sugestão de tier genérica (Azure/GCP/AWS) ──────────────────────────────────
//
// SuggestVMTier substitui SuggestAlternatives (removida) — mesma lógica de detecção de gargalo
// (isMemBottleneck/isCPUHeavy/isOversized), generalizada pra funcionar com qualquer CloudPricer
// (Azure/GCP/AWS), não mais amarrada a *AzurePricer. provider é "aks"|"gke"|"eks" (mesmas
// constantes de config.CloudProviderAKS/GKE/EKS, evita inventar um enum novo).
func SuggestVMTier(provider, currentSKU string, cpuUtilPct, memUtilPct float64, pricer CloudPricer, exchangeRate float64, nodeCount int) []VMAlternative {
	if pricer == nil {
		return nil
	}
	currentCPU, currentMem := pricer.GetVMSpecs(currentSKU)
	if currentCPU == 0 || currentMem == 0 {
		return nil
	}
	currentPrice, _, priceErr := pricer.GetPrice(currentSKU)
	if priceErr != nil || currentPrice <= 0 {
		return nil
	}

	hasUsage := cpuUtilPct > 0 || memUtilPct > 0
	isMemBottleneck := hasUsage && memUtilPct > 65 && memUtilPct > cpuUtilPct*1.3
	isCPUHeavy := hasUsage && cpuUtilPct > 65 && cpuUtilPct > memUtilPct*1.3
	isOversized := hasUsage && cpuUtilPct < 30 && memUtilPct < 30

	type candid struct {
		sku     string
		verdict string
	}
	var candidates []candid

	switch provider {
	case "gke":
		family, _, _, ok := parseGCEMachineType(currentSKU)
		if !ok {
			return nil
		}
		// parseGCEMachineType não devolve o tier (standard/highmem/highcpu) separadamente —
		// extrai direto do nome (2º segmento, "<família>-<tier>-<vCPUs>").
		tier := ""
		if parts := strings.Split(strings.ToLower(currentSKU), "-"); len(parts) >= 2 {
			tier = parts[1]
		}
		switch {
		case isMemBottleneck && tier != "highmem":
			candidates = append(candidates, candid{fmt.Sprintf("%s-highmem-%d", family, currentCPU), "recommended"})
		case isCPUHeavy && tier != "highcpu":
			candidates = append(candidates, candid{fmt.Sprintf("%s-highcpu-%d", family, currentCPU), "consider"})
		case isOversized:
			if half := nearestLadderSize(gcpTierVCPULadder, currentCPU/2); half > 0 && half < currentCPU {
				candidates = append(candidates, candid{fmt.Sprintf("%s-%s-%d", family, tier, half), "cheaper"})
			}
		}

	case "eks":
		family, size, ok := awsParseInstanceType(currentSKU)
		if !ok {
			return nil
		}
		group := awsFamilyGroupOf(family)
		idx := awsSizeIndex(size)
		switch {
		case isMemBottleneck && group != "memory":
			for _, f := range awsFamilyGroups["memory"] {
				candidates = append(candidates, candid{f + "." + size, "recommended"})
			}
		case isCPUHeavy && group != "compute":
			for _, f := range awsFamilyGroups["compute"] {
				candidates = append(candidates, candid{f + "." + size, "consider"})
			}
		case isOversized && idx > 0:
			candidates = append(candidates, candid{family + "." + awsSizesBySize[idx-1], "cheaper"})
		}

	default: // "aks" (também usado como fallback genérico)
		letter := azureFamilyLetter(currentSKU)
		currentMemPerCPU := currentMem / currentCPU
		switch {
		case isMemBottleneck && currentMemPerCPU <= 2:
			candidates = append(candidates, candid{pickAzureBySize("D", currentCPU), "recommended"})
		case isMemBottleneck && currentMemPerCPU <= 4:
			candidates = append(candidates, candid{pickAzureBySize("E", currentCPU), "recommended"})
		case isCPUHeavy && currentMemPerCPU >= 4:
			candidates = append(candidates, candid{pickAzureBySize("F", currentCPU), "consider"})
		case isOversized && currentCPU >= 4:
			candidates = append(candidates, candid{pickAzureBySize(letter, currentCPU/2), "cheaper"})
		}
	}

	seen := map[string]bool{strings.ToLower(currentSKU): true}
	var result []VMAlternative
	for _, c := range candidates {
		if len(result) >= 3 || c.sku == "" {
			continue
		}
		key := strings.ToLower(c.sku)
		if seen[key] {
			continue
		}
		seen[key] = true

		cpu, mem := pricer.GetVMSpecs(c.sku)
		if cpu == 0 {
			continue
		}
		price, src, err := pricer.GetPrice(c.sku)
		if err != nil || price <= 0 {
			continue
		}

		costDeltaPct := (price - currentPrice) / currentPrice * 100
		monthlySavings := (currentPrice - price) * HoursPerMonth * float64(nodeCount) * exchangeRate

		result = append(result, VMAlternative{
			VMSize:            c.sku,
			CPUCores:          cpu,
			MemoryGB:          mem,
			MemPerCPUGB:       math.Round(float64(mem)/float64(cpu)*10) / 10,
			PriceUSDHour:      price,
			PriceSource:       src,
			CostDeltaPct:      math.Round(costDeltaPct*10) / 10,
			MonthlySavingsBRL: math.Round(monthlySavings*100) / 100,
			Reason:            buildAltReason(currentCPU, currentMem, cpu, mem, int(cpuUtilPct), int(memUtilPct)),
			Verdict:           c.verdict,
		})
	}
	return result
}

// pickAzureBySize acha, dentro de uma letra de família, o spec com vCPUs mais próximo do alvo
// (>= quando possível, senão o maior disponível abaixo) — evita string formatting às cegas
// (que podia gerar um nome de SKU que não existe de verdade, ex: "Standard_D3s_v5").
func pickAzureBySize(letter string, targetVCPUs int) string {
	tiers := azureFamilyTiers(letter)
	if len(tiers) == 0 {
		return ""
	}
	best := tiers[0]
	for _, t := range tiers {
		if t.VCPUs >= targetVCPUs {
			best = t
			break
		}
		best = t
	}
	return best.Size
}

// nearestLadderSize retorna o maior valor da ladder que seja <= target (0 se nenhum servir).
func nearestLadderSize(ladder []int, target int) int {
	best := 0
	for _, v := range ladder {
		if v <= target && v > best {
			best = v
		}
	}
	return best
}
