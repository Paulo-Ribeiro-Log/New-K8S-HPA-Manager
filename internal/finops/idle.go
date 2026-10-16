package finops

import "math"

// Capacidade ociosa por pool — porte de computePoolUsage (components/finops/capacity.ts), para a
// visão multi-cluster: o pool está tão ocupado quanto o recurso mais cheio (CPU ou memória),
// contando o maior entre request e uso médio, sobre a capacidade ALOCÁVEL (nominal em relatório
// antigo); pods por pool pelo pool_pods (DaemonSet). Mantenha as duas implementações iguais.

func poolPerNode(p FinOpsPool) (cpu, mem float64) {
	if p.AllocatableNodes > 0 && p.AllocatableCPUMillicores > 0 && p.AllocatableMemoryMi > 0 {
		return float64(p.AllocatableCPUMillicores) / float64(p.AllocatableNodes), float64(p.AllocatableMemoryMi) / float64(p.AllocatableNodes)
	}
	return float64(p.VMCPUCores) * 1000, float64(p.VMMemoryGB) * 1024
}

// PoolOccupancy devolve, por pool, a ocupação (0..1) e se há workload atribuído.
func PoolOccupancy(r *FinOpsReport) map[string]float64 {
	type acc struct {
		reqCPU, reqMem, useCPU, useMem float64
		n                              int
	}
	by := map[string]*acc{}
	names := map[string]bool{}
	for _, p := range r.NodePools {
		names[p.Name] = true
	}
	for _, w := range r.Workloads {
		pods := w.PoolPods
		if len(pods) == 0 && w.NodePool != "" {
			pods = map[string]int{w.NodePool: w.Pods}
		}
		for pool, n := range pods {
			if !names[pool] {
				continue
			}
			a := by[pool]
			if a == nil {
				a = &acc{}
				by[pool] = a
			}
			f := float64(n)
			a.reqCPU += w.CPURequestMillis * f
			a.reqMem += w.MemRequestMi * f
			a.useCPU += w.CPUAvgMillis * f
			a.useMem += w.MemAvgMi * f
			a.n++
		}
	}
	out := map[string]float64{}
	for _, p := range r.NodePools {
		a := by[p.Name]
		if a == nil || a.n == 0 {
			continue
		}
		cpu, mem := poolPerNode(p)
		capCPU, capMem := cpu*float64(p.NodeCount), mem*float64(p.NodeCount)
		if capCPU <= 0 || capMem <= 0 {
			continue // capacidade desconhecida: sem número de ociosa (não é 100% ociosa)
		}
		occ := func(v, c float64) float64 {
			if c <= 0 {
				return 0
			}
			return math.Min(1, v/c)
		}
		out[p.Name] = math.Max(occ(math.Max(a.reqCPU, a.useCPU), capCPU), occ(math.Max(a.reqMem, a.useMem), capMem))
	}
	return out
}

// IdleBRL é a capacidade ociosa do cluster no custo efetivo (soma dos pools com workload).
func IdleBRL(r *FinOpsReport) float64 {
	factor, _ := poolFactors(r)
	occ := PoolOccupancy(r)
	total := 0.0
	for _, p := range r.NodePools {
		o, ok := occ[p.Name]
		if !ok {
			continue
		}
		total += p.MonthlyCostBRL * factor[p.Name] * (1 - o)
	}
	return round2(total)
}
