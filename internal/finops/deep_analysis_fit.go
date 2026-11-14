package finops

import (
	"math"
	"sort"
)

// DeepKindCount é o inventário de workloads do pool por tipo (Deployment, StatefulSet, ...).
type DeepKindCount struct {
	Kind      string `json:"kind"`
	Workloads int    `json:"workloads"`
	Pods      int    `json:"pods"`
}

// DeepInventory é o que roda HOJE no pool, medido: workloads por tipo, DaemonSets, o maior pod e a ocupação média
// dos nodes atuais. É a régua para ver o que cabe num node de outra SKU.
type DeepInventory struct {
	Kinds         []DeepKindCount `json:"kinds"`
	DaemonSets    int             `json:"daemonsets"`
	DSPodsPerNode float64         `json:"ds_pods_per_node"`
	TotalPods     int             `json:"total_pods"` // workloads + DaemonSets (todos os pods do pool)
	Nodes         int             `json:"nodes"`
	AvgPodsNode   float64         `json:"avg_pods_per_node"`
	AvgCPUReqPct  float64         `json:"avg_cpu_req_pct"`
	AvgMemReqPct  float64         `json:"avg_mem_req_pct"`
	// Maior pod (por request): o que mais dificulta o encaixe.
	BiggestPod      string  `json:"biggest_pod,omitempty"`
	BiggestPodCPU   float64 `json:"biggest_pod_cpu_millis,omitempty"`
	BiggestPodMemMi float64 `json:"biggest_pod_mem_mi,omitempty"`
	AvgPodCPUMillis float64 `json:"avg_pod_cpu_millis"` // média dos requests recomendados por pod
	AvgPodMemMi     float64 `json:"avg_pod_mem_mi"`
}

// DeepNodeFit é o que cabe em UM node desta SKU e quanto dele seria ocupado, no cenário de requests recomendados.
type DeepNodeFit struct {
	// DaemonSets (fixos em todo node): pods e quanto do alocável ocupam.
	DSPods   float64 `json:"ds_pods"`
	DSCPUPct float64 `json:"ds_cpu_pct"`
	DSMemPct float64 `json:"ds_mem_pct"`
	// Pods de aplicação que cabem em um node com o pod médio do pool, por recurso; Fits é o menor deles.
	PodsByCPU   int    `json:"pods_by_cpu"`
	PodsByMem   int    `json:"pods_by_mem"`
	PodsByLimit int    `json:"pods_by_limit"` // vagas de pods (máx. por node − DaemonSets)
	PodsFit     int    `json:"pods_fit"`
	FitLimiting string `json:"fit_limiting"`
	// Quantos do MAIOR pod cabem num node.
	BiggestFits   int     `json:"biggest_fits"`
	BiggestPctCPU float64 `json:"biggest_pct_cpu"`
	BiggestPctMem float64 `json:"biggest_pct_mem"`
	// Ocupação de cada node com os nodes recomendados (DaemonSets + a parte dos workloads), em % do alocável/limite.
	OccCPUPct   float64 `json:"occ_cpu_pct"`
	OccMemPct   float64 `json:"occ_mem_pct"`
	OccPodsPct  float64 `json:"occ_pods_pct"`
	PodsPerNode float64 `json:"pods_per_node"` // pods de aplicação por node nessa distribuição
}

// nodeRows (opcional) traz a ocupação medida dos nodes atuais.
func buildInventory(in DeepAnalysisInput, ds DeepDaemonSetOverhead, workloads []DeepWorkloadRow, nodeRows []DeepNodeRow) DeepInventory {
	inv := DeepInventory{Kinds: []DeepKindCount{}, DaemonSets: len(ds.Items), DSPodsPerNode: ds.PerNodePods, Nodes: len(in.Nodes)}
	byKind := map[string]*DeepKindCount{}
	var cpuSum, memSum float64
	var bigScore float64
	for _, w := range workloads {
		k := w.Kind
		if k == "" {
			k = "Outros"
		}
		kc := byKind[k]
		if kc == nil {
			kc = &DeepKindCount{Kind: k}
			byKind[k] = kc
		}
		kc.Workloads++
		kc.Pods += w.PodsOnPool
		inv.TotalPods += w.PodsOnPool
		cpuSum += w.CPURecMillis * float64(w.PodsOnPool)
		memSum += w.MemRecMi * float64(w.PodsOnPool)
		// o "maior pod" é o de maior fração de um node médio do pool
		if len(in.Nodes) > 0 {
			var ac, am float64
			for _, n := range in.Nodes {
				ac += n.CPUAllocMillis
				am += n.MemAllocMi
			}
			ac /= float64(len(in.Nodes))
			am /= float64(len(in.Nodes))
			sc := 0.0
			if ac > 0 {
				sc = w.CPUReqMillis / ac
			}
			if am > 0 {
				sc = math.Max(sc, w.MemReqMi/am)
			}
			if sc > bigScore {
				bigScore = sc
				inv.BiggestPod, inv.BiggestPodCPU, inv.BiggestPodMemMi = w.Namespace+"/"+w.Workload, w.CPUReqMillis, w.MemReqMi
			}
		}
	}
	for _, kc := range byKind {
		inv.Kinds = append(inv.Kinds, *kc)
	}
	sort.Slice(inv.Kinds, func(i, j int) bool { return inv.Kinds[i].Pods > inv.Kinds[j].Pods })
	if inv.TotalPods > 0 {
		inv.AvgPodCPUMillis = round2(cpuSum / float64(inv.TotalPods))
		inv.AvgPodMemMi = round2(memSum / float64(inv.TotalPods))
	}
	if n := len(nodeRows); n > 0 {
		var pods, cp, mp float64
		for _, x := range nodeRows {
			pods += float64(x.Pods)
			cp += x.CPUReqPct
			mp += x.MemReqPct
		}
		inv.AvgPodsNode, inv.AvgCPUReqPct, inv.AvgMemReqPct = round2(pods/float64(n)), round2(cp/float64(n)), round2(mp/float64(n))
	}
	inv.TotalPods += int(math.Round(ds.PerNodePods * float64(len(in.Nodes))))
	return inv
}

func fitFloor(free, per float64) int {
	if per <= 0 || free <= 0 {
		return 0
	}
	return int(math.Floor(free / per))
}

// buildNodeFit calcula o encaixe em um node da SKU s: o que os DaemonSets ocupam, quantos pods médios e quantos do
// maior pod cabem, e a ocupação resultante com os nodes recomendados.
func buildNodeFit(s DeepSimulation, inv DeepInventory, ds DeepDaemonSetOverhead, maxPods int, recDemand deepDemand) *DeepNodeFit {
	if !s.Feasible || s.AllocCPUMillis <= 0 || s.AllocMemMi <= 0 {
		return nil
	}
	f := &DeepNodeFit{DSPods: ds.PerNodePods}
	f.DSCPUPct = round2(ds.PerNodeCPUReqMillis / s.AllocCPUMillis * 100)
	f.DSMemPct = round2(math.Max(ds.PerNodeMemReqMi, ds.PerNodeMemUsageMi) / s.AllocMemMi * 100)
	f.PodsByCPU = fitFloor(s.FreeCPUMillis, inv.AvgPodCPUMillis)
	f.PodsByMem = fitFloor(s.FreeMemMi, inv.AvgPodMemMi)
	f.PodsByLimit = int(math.Max(0, math.Floor(s.FreePods)))
	f.PodsFit, f.FitLimiting = f.PodsByCPU, "cpu"
	if f.PodsByMem < f.PodsFit {
		f.PodsFit, f.FitLimiting = f.PodsByMem, "memory"
	}
	if f.PodsByLimit < f.PodsFit {
		f.PodsFit, f.FitLimiting = f.PodsByLimit, "pods"
	}
	if inv.BiggestPod != "" {
		f.BiggestFits = int(math.Min(float64(fitFloor(s.FreeCPUMillis, inv.BiggestPodCPU)), float64(fitFloor(s.FreeMemMi, inv.BiggestPodMemMi))))
		f.BiggestPctCPU = round2(inv.BiggestPodCPU / s.AllocCPUMillis * 100)
		f.BiggestPctMem = round2(inv.BiggestPodMemMi / s.AllocMemMi * 100)
	}
	if n := float64(s.NodesRecommended); n > 0 && maxPods > 0 {
		f.PodsPerNode = round2(float64(recDemand.pods) / n)
		f.OccCPUPct = round2((ds.PerNodeCPUReqMillis + recDemand.cpuMillis/n) / s.AllocCPUMillis * 100)
		f.OccMemPct = round2((math.Max(ds.PerNodeMemReqMi, ds.PerNodeMemUsageMi) + recDemand.memMi/n) / s.AllocMemMi * 100)
		f.OccPodsPct = round2((ds.PerNodePods + f.PodsPerNode) / float64(maxPods) * 100)
	}
	return f
}
