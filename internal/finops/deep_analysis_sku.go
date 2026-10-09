package finops

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// DeepSKUSpec descreve uma SKU de VM para a simulação da Deep Analysis: vCPU, memória, CPU e SMT.
//
// Tabela própria (e não GetVMSpecs) porque a inferência por nome de GetVMSpecs trata toda a família F
// como 2 GB/vCPU — errado para Fas_v6 (4 GB/vCPU) e Fams_v6 (8 GB/vCPU), justamente as SKUs que mais
// interessam numa troca orientada a latência. O catálogo de capacidades da região (sku_catalog.go) pode
// sobrepor estes valores com os reais da região.
type DeepSKUSpec struct {
	VMSize string `json:"vm_size"`
	VCPU   int    `json:"vcpu"`
	MemGB  int    `json:"mem_gb"`
	// CPU é o processador da série segundo a documentação da Azure (o SKU não fixa o processador
	// exato: F_v2, por exemplo, roda em Skylake ou Cascade Lake conforme o host).
	CPU string `json:"cpu,omitempty"`
	// SMT: cada vCPU é uma thread de hyperthreading (2 vCPUs por core físico). false = 1 vCPU por
	// core físico (Fas_v6/Fals_v6/Fams_v6), relevante para latência.
	SMT    bool   `json:"smt"`
	Series string `json:"series,omitempty"`
}

// azureDeepSeries são as séries consideradas como candidatas na simulação (AKS). format recebe o nº de
// vCPUs (ex.: "Standard_D%das_v5" → "Standard_D4as_v5").
var azureDeepSeries = []struct {
	format    string
	series    string
	memPerCPU int
	cpu       string
	smt       bool
	sizes     []int
}{
	{"Standard_D%ds_v5", "Dsv5", 4, "Intel Xeon Ice Lake", true, []int{2, 4, 8, 16, 32, 48, 64, 96}},
	{"Standard_D%das_v5", "Dasv5", 4, "AMD EPYC Milan", true, []int{2, 4, 8, 16, 32, 48, 64, 96}},
	{"Standard_D%ds_v6", "Dsv6", 4, "Intel Xeon Emerald Rapids", true, []int{2, 4, 8, 16, 32, 48, 64, 96, 128}},
	{"Standard_D%das_v6", "Dasv6", 4, "AMD EPYC Genoa", true, []int{2, 4, 8, 16, 32, 48, 64, 96}},
	{"Standard_E%ds_v5", "Esv5", 8, "Intel Xeon Ice Lake", true, []int{2, 4, 8, 16, 20, 32, 48, 64, 96}},
	{"Standard_E%das_v5", "Easv5", 8, "AMD EPYC Milan", true, []int{2, 4, 8, 16, 20, 32, 48, 64, 96}},
	{"Standard_F%das_v6", "Fasv6", 4, "AMD EPYC Genoa", false, []int{2, 4, 8, 16, 32, 48, 64}},
	{"Standard_F%dals_v6", "Falsv6", 2, "AMD EPYC Genoa", false, []int{2, 4, 8, 16, 32, 48, 64}},
	{"Standard_F%dams_v6", "Famsv6", 8, "AMD EPYC Genoa", false, []int{2, 4, 8, 16, 32, 48, 64}},
	{"Standard_F%ds_v2", "Fsv2", 2, "Intel Xeon Skylake/Cascade Lake", true, []int{2, 4, 8, 16, 32, 48, 64, 72}},
	{"Standard_D%ds_v4", "Dsv4", 4, "Intel Xeon Cascade Lake", true, []int{2, 4, 8, 16, 32, 48, 64}},
	{"Standard_D%dds_v4", "Ddsv4", 4, "Intel Xeon Cascade Lake", true, []int{2, 4, 8, 16, 32, 48, 64}},
}

// DeepSpecFor devolve a spec de uma SKU Azure conhecida pela tabela da Deep Analysis (comparação sem
// diferenciar maiúsculas/minúsculas).
func DeepSpecFor(vmSize string) (DeepSKUSpec, bool) {
	want := strings.ToLower(strings.TrimSpace(vmSize))
	for _, s := range azureDeepSeries {
		for _, n := range s.sizes {
			name := fmt.Sprintf(s.format, n)
			if strings.ToLower(name) == want {
				return DeepSKUSpec{VMSize: name, VCPU: n, MemGB: n * s.memPerCPU, CPU: s.cpu, SMT: s.smt, Series: s.series}, true
			}
		}
	}
	return DeepSKUSpec{}, false
}

// DeepCandidateSKUs lista as SKUs Azure candidatas para um pool com currentVCPU vCPUs por node: o mesmo
// nº de vCPU em todas as séries da tabela e o dobro (até 16 vCPUs, nós maiores diluem melhor o custo
// fixo de DaemonSets). Não inclui a SKU atual (o chamador a acrescenta com o alocável medido).
func DeepCandidateSKUs(currentSKU string, currentVCPU int) []DeepSKUSpec {
	if currentVCPU <= 0 {
		return nil
	}
	targets := []int{currentVCPU}
	if double := currentVCPU * 2; double <= 16 {
		targets = append(targets, double)
	}
	cur := strings.ToLower(currentSKU)
	var out []DeepSKUSpec
	for _, n := range targets {
		for _, s := range azureDeepSeries {
			if !containsInt(s.sizes, n) {
				continue
			}
			name := fmt.Sprintf(s.format, n)
			if strings.ToLower(name) == cur {
				continue
			}
			out = append(out, DeepSKUSpec{VMSize: name, VCPU: n, MemGB: n * s.memPerCPU, CPU: s.cpu, SMT: s.smt, Series: s.series})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].VCPU < out[j].VCPU })
	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// aksReservedCPUMillis é a CPU reservada pela AKS (kube-reserved) por node, conforme a tabela da
// documentação: 1 core → 60m, 2 → 100m, 4 → 140m e +10m por core acima de 4 (8 → 180m, 16 → 260m,
// 32 → 420m, 64 → 740m). Validado no calculofrete: F4s_v2 = 4000m − 140m = 3860m alocáveis.
func aksReservedCPUMillis(vcpu int) float64 {
	switch {
	case vcpu <= 1:
		return 60
	case vcpu == 2:
		return 100
	case vcpu == 3:
		return 120
	default:
		return 140 + 10*float64(vcpu-4)
	}
}

// aksEvictionMi é o limiar de eviction de memória do kubelet na AKS (memory.available < 100Mi).
const aksEvictionMi = 100

// aksReservedMemMi é a memória reservada pela AKS (Kubernetes ≥ 1.29): min(20 MB × maxPods + 50 MB,
// 25% da memória) + o limiar de eviction. Os MB da fórmula são decimais.
func aksReservedMemMi(capMi float64, maxPods int) float64 {
	const mbToMi = 1e6 / (1 << 20)
	reserved := (20*float64(maxPods) + 50) * mbToMi
	if quarter := capMi * 0.25; reserved > quarter {
		reserved = quarter
	}
	return reserved + aksEvictionMi
}

// EstimateAllocatable estima o alocável (CPU em millicores, memória em Mi) de um node da SKU, pela
// fórmula de reserva da AKS. memFactor corrige a memória nominal para a capacidade que o node de fato
// expõe ao Kubernetes (medida no pool atual; ver deepMemFactor).
func EstimateAllocatable(spec DeepSKUSpec, maxPods int, memFactor float64) (cpuMillis, memMi float64) {
	capCPU := float64(spec.VCPU) * 1000
	capMem := float64(spec.MemGB) * 1024 * memFactor
	cpuMillis = math.Max(0, capCPU-aksReservedCPUMillis(spec.VCPU))
	memMi = math.Max(0, capMem-aksReservedMemMi(capMem, maxPods))
	return cpuMillis, memMi
}

// deepMemFactor é a razão entre a memória que o node expõe (capacity) e a memória nominal da SKU,
// medida no pool atual e limitada a [0,90; 1,00]. Sem medição, 0,98.
func deepMemFactor(measuredCapMi float64, nominalGB int) float64 {
	if measuredCapMi <= 0 || nominalGB <= 0 {
		return 0.98
	}
	f := measuredCapMi / (float64(nominalGB) * 1024)
	return math.Min(1, math.Max(0.9, f))
}
